package logic

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/google/uuid"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/modules/asset/service"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
	"github.com/JarvanDante/my_media/internal/shared/mq"
	"github.com/JarvanDante/my_media/internal/shared/storage"
	"github.com/JarvanDante/my_media/internal/shared/transcode"
)

type sAsset struct {
	repo  domain.Repository
	store *storage.Minio
	bus   *mq.Bus
}

func New(repo domain.Repository, store *storage.Minio, bus *mq.Bus) service.Asset {
	return &sAsset{repo: repo, store: store, bus: bus}
}

func (s *sAsset) List(ctx context.Context, f domain.ListFilter) ([]domain.Asset, int, error) {
	return s.repo.List(ctx, f)
}

func (s *sAsset) Create(ctx context.Context, title, coverUrl, remark string) (int64, error) {
	return s.repo.Create(ctx, title, coverUrl, remark)
}

func (s *sAsset) Get(ctx context.Context, id int64) (*domain.Asset, error) {
	return s.repo.Get(ctx, id)
}

func (s *sAsset) Pick(ctx context.Context, appKey, siteCode string, assetID int64) (*domain.Asset, error) {
	return s.repo.Pick(ctx, appKey, siteCode, assetID)
}

func (s *sAsset) PresignUpload(ctx context.Context, assetID int64, filename string) (*v1.UploadURLRes, error) {
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	a, err := s.repo.Get(ctx, assetID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = ".mp4"
	}
	key := fmt.Sprintf("media/source/%d/%s%s", assetID, uuid.NewString(), ext)
	bucket := s.store.Bucket()
	url, err := s.store.PresignPut(ctx, bucket, key)
	if err != nil {
		return nil, gerror.Wrap(err, "生成预签名失败")
	}
	if err := s.repo.BindSource(ctx, assetID, bucket, key); err != nil {
		return nil, err
	}
	return &v1.UploadURLRes{
		UploadUrl: url,
		Method:    "PUT",
		Bucket:    bucket,
		Key:       key,
		ExpireSec: g.Cfg().MustGet(ctx, "minio.presign_expire_sec", 7200).Int(),
	}, nil
}

func (s *sAsset) TriggerTranscode(ctx context.Context, assetID int64) (string, error) {
	if s.bus == nil || !s.bus.Enabled() {
		return "", gerror.NewCode(errcode.CodeBadRequest, "Kafka 未启用")
	}
	if s.store == nil {
		return "", gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	a, err := s.repo.Get(ctx, assetID)
	if err != nil {
		return "", err
	}
	if a == nil {
		return "", gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	if a.SourceBucket == "" || a.SourceKey == "" {
		return "", gerror.NewCode(errcode.CodeBadRequest, "请先获取上传地址并上传原片")
	}
	if err := s.store.Stat(ctx, a.SourceBucket, a.SourceKey); err != nil {
		return "", gerror.NewCode(errcode.CodeBadRequest, "原片尚未上传或不存在，请先 PUT 到预签名地址")
	}

	profile := g.Cfg().MustGet(ctx, "transcode.profile", transcode.ProfileH264HLS).String()
	jobID := fmt.Sprintf("media_asset_%d_%d", assetID, time.Now().Unix())
	prefix := fmt.Sprintf("media/hls/%d/", assetID)

	job := transcode.JobMessage{
		SchemaVersion: 1,
		JobID:         jobID,
		Biz:           transcode.BizMedia,
		BizRef:        transcode.BizRefAsset(assetID),
		Input:         transcode.ObjectRef{Bucket: a.SourceBucket, Key: a.SourceKey},
		Output:        transcode.OutputRef{Bucket: a.SourceBucket, Prefix: prefix},
		Profile:       profile,
		CreatedAt:     time.Now().Format(time.RFC3339),
	}

	if err := s.repo.MarkTranscoding(ctx, assetID, jobID, profile); err != nil {
		return "", err
	}
	if err := s.bus.PublishJob(ctx, job); err != nil {
		return "", gerror.Wrap(err, "投递转码任务失败")
	}
	return jobID, nil
}

func (s *sAsset) HandleTranscodeResult(ctx context.Context, msg transcode.ResultMessage) error {
	if msg.Biz != "" && msg.Biz != transcode.BizMedia {
		return nil
	}
	playURL := msg.PlayURL
	if playURL == "" && msg.PlayKey != "" && s.store != nil {
		// Result 可能只带 play_key；用本服务 public_base 拼
		bucket := s.store.Bucket()
		playURL = s.store.PublicURL(bucket, msg.PlayKey)
	}
	return s.repo.ApplyTranscodeResult(ctx, domain.TranscodeResult{
		JobID:       msg.JobID,
		Status:      msg.Status,
		PlayKey:     msg.PlayKey,
		PlayURL:     playURL,
		DurationSec: msg.DurationSec,
		Error:       msg.Error,
	})
}
