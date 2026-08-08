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

func (s *sAsset) Create(ctx context.Context, title, coverUrl, remark string) (string, error) {
	return s.repo.Create(ctx, title, coverUrl, remark)
}

func (s *sAsset) Get(ctx context.Context, code string) (*domain.Asset, error) {
	return s.repo.GetByCode(ctx, code)
}

func (s *sAsset) Delete(ctx context.Context, code string) (int, error) {
	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return 0, err
	}
	if a == nil {
		return 0, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}

	if s.store == nil {
		return 0, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化，拒绝删除以免残留对象")
	}
	bucket := a.SourceBucket
	if bucket == "" {
		bucket = s.store.Bucket()
	}
	prefixes := objectPrefixesForAsset(a)
	deleted := 0
	for _, prefix := range prefixes {
		n, remErr := s.store.RemovePrefix(ctx, bucket, prefix)
		deleted += n
		if remErr != nil {
			return deleted, gerror.Wrapf(remErr, "清理对象存储失败(%s)，已中止，库记录未删", prefix)
		}
	}
	// 兼容落在前缀外的单个原片 key
	if a.SourceKey != "" && !underAnyPrefix(a.SourceKey, prefixes) {
		if remErr := s.store.RemoveObject(ctx, bucket, a.SourceKey); remErr != nil {
			return deleted, gerror.Wrapf(remErr, "清理原片失败(%s)，已中止，库记录未删", a.SourceKey)
		}
		deleted++
	}

	if err := s.repo.Delete(ctx, a.Pk); err != nil {
		return deleted, gerror.Wrap(err, "对象已清理但删除库记录失败，请重试或人工核对")
	}
	return deleted, nil
}

func objectPrefixesForAsset(a *domain.Asset) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(p string) {
		p = strings.TrimLeft(strings.TrimSpace(p), "/")
		if p == "" {
			return
		}
		if !strings.HasSuffix(p, "/") {
			p += "/"
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if a.Code != "" {
		add("media/source/" + a.Code)
		add("media/hls/" + a.Code)
	}
	// 兼容早期用数字主键拼路径：media/hls/4/index.m3u8
	if a.PlayKey != "" {
		dir := filepath.ToSlash(filepath.Dir(a.PlayKey))
		if dir != "" && dir != "." {
			add(dir)
		}
	}
	if a.SourceKey != "" {
		dir := filepath.ToSlash(filepath.Dir(a.SourceKey))
		if dir != "" && dir != "." {
			add(dir)
		}
	}
	return out
}

func underAnyPrefix(key string, prefixes []string) bool {
	key = strings.TrimLeft(key, "/")
	for _, p := range prefixes {
		if strings.HasPrefix(key, p) {
			return true
		}
	}
	return false
}

func (s *sAsset) Pick(ctx context.Context, appKey, siteCode, code string) (*domain.Asset, error) {
	return s.repo.Pick(ctx, appKey, siteCode, code)
}

func (s *sAsset) PresignUpload(ctx context.Context, code, filename string) (*v1.UploadURLRes, error) {
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	a, err := s.repo.GetByCode(ctx, code)
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
	key := fmt.Sprintf("media/source/%s/%s%s", code, uuid.NewString(), ext)
	bucket := s.store.Bucket()
	url, err := s.store.PresignPut(ctx, bucket, key)
	if err != nil {
		return nil, gerror.Wrap(err, "生成预签名失败")
	}
	if err := s.repo.BindSource(ctx, a.Pk, bucket, key); err != nil {
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

func (s *sAsset) TriggerTranscode(ctx context.Context, code string, coverSeekSec int) (string, error) {
	if s.bus == nil || !s.bus.Enabled() {
		return "", gerror.NewCode(errcode.CodeBadRequest, "Kafka 未启用")
	}
	if s.store == nil {
		return "", gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	a, err := s.repo.GetByCode(ctx, code)
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

	if coverSeekSec <= 0 {
		coverSeekSec = g.Cfg().MustGet(ctx, "transcode.cover_seek_sec", 8).Int()
	}
	if coverSeekSec <= 0 {
		coverSeekSec = 8
	}

	profile := g.Cfg().MustGet(ctx, "transcode.profile", transcode.ProfileH264HLS).String()
	jobID := fmt.Sprintf("media_%s_%d", code, time.Now().Unix())
	prefix := fmt.Sprintf("media/hls/%s/", code)

	job := transcode.JobMessage{
		SchemaVersion: 1,
		JobID:         jobID,
		Biz:           transcode.BizMedia,
		BizRef:        "asset:" + code,
		Input:         transcode.ObjectRef{Bucket: a.SourceBucket, Key: a.SourceKey},
		Output:        transcode.OutputRef{Bucket: a.SourceBucket, Prefix: prefix},
		Profile:       profile,
		CoverSeekSec:  coverSeekSec,
		CreatedAt:     time.Now().Format(time.RFC3339),
	}

	if err := s.repo.MarkTranscoding(ctx, a.Pk, jobID, profile); err != nil {
		return "", err
	}
	if err := s.bus.PublishJob(ctx, job); err != nil {
		return "", gerror.Wrap(err, "投递转码任务失败")
	}
	return jobID, nil
}

func (s *sAsset) ListPicks(ctx context.Context, appKey string, page, size int) ([]domain.PickRecord, int, error) {
	return s.repo.ListPicks(ctx, appKey, page, size)
}

func (s *sAsset) PickedSet(ctx context.Context, appKey string, codes []string) (map[string]bool, error) {
	return s.repo.PickedSet(ctx, appKey, codes)
}

func (s *sAsset) HandleTranscodeResult(ctx context.Context, msg transcode.ResultMessage) error {
	if msg.Biz != "" && msg.Biz != transcode.BizMedia {
		return nil
	}
	playURL := msg.PlayURL
	if playURL == "" && msg.PlayKey != "" && s.store != nil {
		bucket := s.store.Bucket()
		playURL = s.store.PublicURL(bucket, msg.PlayKey)
	}
	coverURL := msg.CoverURL
	if coverURL == "" && msg.CoverKey != "" && s.store != nil {
		coverURL = s.store.PublicURL(s.store.Bucket(), msg.CoverKey)
	}
	return s.repo.ApplyTranscodeResult(ctx, domain.TranscodeResult{
		JobID:       msg.JobID,
		Status:      msg.Status,
		PlayKey:     msg.PlayKey,
		PlayURL:     playURL,
		CoverURL:    coverURL,
		DurationSec: msg.DurationSec,
		Error:       msg.Error,
	})
}
