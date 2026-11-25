package logic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
	"github.com/JarvanDante/my_media/internal/shared/storage"
)

const dramaUploadExpire = 24 * time.Hour

// dramaComposeTimeout 与短剧导入单次 HTTP 默认时限一致，迟到 Compose 不会超出补扫窗口。
var dramaComposeTimeout = time.Duration(defaultImportTimeoutSec) * time.Second

// dramaPurgeTimeout 会话对象清理脱离请求取消后的上限。
const dramaPurgeTimeout = time.Minute

func (s *sAsset) uploadDB() gdb.DB {
	if s.jobDB != nil {
		return s.jobDB
	}
	return g.DB()
}

// dramaAbortSweepWindow 覆盖预签名有效期与两倍 Compose 时限，窗口内的已取消会话会被反复补扫。
func dramaAbortSweepWindow(ctx context.Context) time.Duration {
	sec := g.Cfg().MustGet(ctx, "minio.presign_expire_sec", 7200).Int64()
	if sec <= 0 {
		sec = 7200
	}
	return time.Duration(sec)*time.Second + 2*dramaComposeTimeout
}

func mergeDramaPlanJSON(planJSON, targetID string) (string, error) {
	targetID = strings.TrimSpace(targetID)
	plan := DramaImportPlan{}
	if strings.TrimSpace(planJSON) != "" {
		if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
			return "", fmt.Errorf("导入计划格式无效")
		}
	}
	if targetID != "" {
		if plan.TargetID == "" {
			plan.TargetID = targetID
		}
		for i := range plan.Items {
			if strings.TrimSpace(plan.Items[i].TargetID) == "" {
				plan.Items[i].TargetID = targetID
			}
		}
	}
	b, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *sAsset) CreateDramaUpload(ctx context.Context, filename string, totalSize int64) (*v1.DramaUploadRes, error) {
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	filename = strings.TrimSpace(filename)
	if strings.ToLower(filepath.Ext(filename)) != ".zip" {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "只支持 .zip")
	}
	lim := dramaImportLimits(ctx)
	if totalSize <= 0 {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "压缩包大小无效")
	}
	if totalSize > lim.MaxZipBytes {
		return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("压缩包大小 %s 超过上限 %s", humanBytes(totalSize), humanBytes(lim.MaxZipBytes)))
	}
	chunk := lim.MaxChunkBytes
	if chunk <= 0 {
		chunk = defaultMaxChunkBytes
	}
	partCount := int((totalSize + chunk - 1) / chunk)
	maxParts := MaxDramaImportParts(lim)
	if partCount > maxParts {
		return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片数量 %d 超过上限 %d", partCount, maxParts))
	}
	up, err := dao.CreateDramaUpload(ctx, filename, totalSize, chunk, partCount, s.store.Bucket(), dramaUploadExpire)
	if err != nil {
		return nil, err
	}
	return s.dramaUploadRes(ctx, up, true)
}

func (s *sAsset) GetDramaUpload(ctx context.Context, id string) (*v1.DramaUploadRes, error) {
	up, err := s.loadDramaUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := s.refreshDramaUploadParts(ctx, up); err != nil {
		return nil, err
	}
	return s.dramaUploadRes(ctx, up, true)
}

func (s *sAsset) loadDramaUpload(ctx context.Context, id string) (*dao.DramaImportUpload, error) {
	if !dao.ValidDramaImportCode(id) {
		return nil, gerror.NewCode(errcode.CodeNotFound, "上传会话不存在")
	}
	up, err := dao.GetDramaUploadByCodeOnDB(s.uploadDB(), ctx, id)
	if err != nil {
		return nil, err
	}
	if up == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "上传会话不存在")
	}
	return up, nil
}

func (s *sAsset) refreshDramaUploadParts(ctx context.Context, up *dao.DramaImportUpload) error {
	store := s.store
	if store == nil || up == nil {
		return nil
	}
	var newly []int
	for i, p := range up.Parts {
		if p.UploadedAt != nil {
			continue
		}
		sz, err := store.ObjectSize(ctx, up.Bucket, p.ObjectKey)
		if err != nil || sz != p.Size {
			continue
		}
		now := time.Now()
		up.Parts[i].UploadedAt = &now
		newly = append(newly, p.N)
	}
	return dao.MarkDramaPartsUploadedOnDB(s.uploadDB(), ctx, up.Pk, newly)
}

func (s *sAsset) dramaUploadRes(ctx context.Context, up *dao.DramaImportUpload, withURLs bool) (*v1.DramaUploadRes, error) {
	res := &v1.DramaUploadRes{
		Id: up.Code, Filename: up.Filename, TotalSize: up.TotalSize, ChunkSize: up.ChunkSize,
		PartCount: up.PartCount, Status: up.Status, JobId: up.JobCode,
		ExpiresAt: up.ExpiresAt.UTC().Format(time.RFC3339),
		Parts:     make([]v1.DramaUploadPart, 0, len(up.Parts)),
	}
	offset := int64(0)
	for _, p := range up.Parts {
		part := v1.DramaUploadPart{
			N: p.N, Offset: offset, Size: p.Size, Done: p.UploadedAt != nil, Mode: "proxy",
		}
		offset += p.Size
		if withURLs && !part.Done && up.Status == dao.DramaUploadUploading && s.store != nil {
			url, err := s.store.PresignPut(ctx, up.Bucket, p.ObjectKey)
			if err != nil {
				g.Log().Warningf(ctx, "drama upload presign part %d: %v", p.N, err)
			} else if url != "" && !storage.PresignHostUnreachable(url) {
				part.Mode = "presign"
				part.Url = url
			}
		}
		res.Parts = append(res.Parts, part)
	}
	return res, nil
}

func (s *sAsset) PutDramaUploadPart(ctx context.Context, id string, n int, body io.Reader, contentLength int64) (*v1.PutDramaUploadPartRes, error) {
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	up, err := s.loadDramaUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if up.Status != dao.DramaUploadUploading {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "上传会话已结束")
	}
	if time.Now().After(up.ExpiresAt) {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "上传会话已过期")
	}
	var part *dao.DramaImportPart
	for i := range up.Parts {
		if up.Parts[i].N == n {
			part = &up.Parts[i]
			break
		}
	}
	if part == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "分片序号无效")
	}
	if !strings.HasPrefix(part.ObjectKey, up.Prefix) || !strings.HasPrefix(up.Prefix, consts.PrefixDrama+"import/"+up.Code+"/") {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "分片对象不属于本会话")
	}
	if contentLength > 0 && contentLength != part.Size {
		return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片 %d 大小应为 %s，实际请求 %s", n, humanBytes(part.Size), humanBytes(contentLength)))
	}
	if part.UploadedAt != nil {
		if sz, err := s.store.ObjectSize(ctx, up.Bucket, part.ObjectKey); err == nil && sz == part.Size {
			return &v1.PutDramaUploadPartRes{N: n, Size: part.Size, Done: true}, nil
		}
	}
	lim := io.LimitReader(body, part.Size+1)
	tmp, err := os.CreateTemp("", "drama-part-")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	written, err := io.Copy(tmp, lim)
	_ = tmp.Close()
	if err != nil {
		return nil, err
	}
	if written != part.Size {
		return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片 %d 大小应为 %s，实际 %s", n, humanBytes(part.Size), humanBytes(written)))
	}
	f, err := os.Open(tmpName)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := s.store.PutObject(ctx, up.Bucket, part.ObjectKey, "application/octet-stream", f, part.Size); err != nil {
		return nil, gerror.Wrap(err, "写入分片失败")
	}
	if err := dao.MarkDramaPartUploaded(ctx, up.Pk, n, part.Size); err != nil {
		return nil, err
	}
	return &v1.PutDramaUploadPartRes{N: n, Size: part.Size, Done: true}, nil
}

func (s *sAsset) CompleteDramaUpload(ctx context.Context, id, planJSON, targetID string) (*v1.CompleteDramaUploadRes, error) {
	store := s.store
	if store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	up, err := s.loadDramaUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if up.Status == dao.DramaUploadAborted {
		return nil, gerror.NewCode(errcode.CodeDramaUploadAborted, "上传会话已取消，请重新上传")
	}
	planJSON, err = mergeDramaPlanJSON(planJSON, targetID)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}
	if job, err := dao.GetDramaImportJobByUploadIDOnDB(s.uploadDB(), ctx, up.Pk); err != nil {
		return nil, err
	} else if job != nil {
		return s.finalizeDramaUpload(ctx, up, job.PackKey, job.PlanJSON)
	}
	if time.Now().After(up.ExpiresAt) && up.Status != dao.DramaUploadCompleted {
		return nil, gerror.NewCode(errcode.CodeDramaUploadExpired, "上传会话已过期")
	}
	if err := s.refreshDramaUploadParts(ctx, up); err != nil {
		return nil, err
	}
	packKey := up.PackKey
	if packKey == "" {
		packKey = up.Prefix + "pack.zip"
	}
	if !strings.HasPrefix(packKey, up.Prefix) {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "整包对象不属于本会话")
	}
	if err := s.composeDramaPack(ctx, store, up, packKey); err != nil {
		return s.afterComposeFailure(ctx, up, err)
	}
	return s.finalizeDramaUpload(ctx, up, packKey, planJSON)
}

func (s *sAsset) composeDramaPack(ctx context.Context, store *storage.Minio, up *dao.DramaImportUpload, packKey string) error {
	if sz, err := store.ObjectSize(ctx, up.Bucket, packKey); err == nil && sz == up.TotalSize {
		return nil
	}
	keys := make([]string, 0, len(up.Parts))
	var got int64
	for _, p := range up.Parts {
		if !strings.HasPrefix(p.ObjectKey, up.Prefix) {
			return gerror.NewCode(errcode.CodeBadRequest, "分片对象不属于本会话")
		}
		sz, err := store.ObjectSize(ctx, up.Bucket, p.ObjectKey)
		if err != nil {
			return gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片 %d 尚未上传完整", p.N))
		}
		if sz != p.Size {
			return gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片 %d 大小不匹配（期望 %s，实际 %s）", p.N, humanBytes(p.Size), humanBytes(sz)))
		}
		keys = append(keys, p.ObjectKey)
		got += sz
	}
	if got != up.TotalSize {
		return gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片合计 %s 与会话总量 %s 不一致", humanBytes(got), humanBytes(up.TotalSize)))
	}
	cctx, cancel := context.WithTimeout(ctx, dramaComposeTimeout)
	defer cancel()
	if err := store.ComposeObjects(cctx, up.Bucket, packKey, keys); err != nil {
		return gerror.Wrap(err, "合并分片失败")
	}
	if sz, err := store.ObjectSize(ctx, up.Bucket, packKey); err != nil || sz != up.TotalSize {
		return gerror.NewCode(errcode.CodeBadRequest, "合并后的整包大小校验失败")
	}
	return nil
}

// afterComposeFailure 合并失败后以库为准：已有任务幂等返回，已取消则补偿清理。
func (s *sAsset) afterComposeFailure(ctx context.Context, up *dao.DramaImportUpload, cause error) (*v1.CompleteDramaUploadRes, error) {
	if job, err := dao.GetDramaImportJobByUploadIDOnDB(s.uploadDB(), ctx, up.Pk); err == nil && job != nil {
		return s.finalizeDramaUpload(ctx, up, job.PackKey, job.PlanJSON)
	}
	if cur, err := dao.GetDramaUploadByCodeOnDB(s.uploadDB(), ctx, up.Code); err == nil && cur != nil && cur.Status == dao.DramaUploadAborted {
		_, _ = s.purgeDramaUploadObjects(ctx, up)
		return nil, gerror.NewCode(errcode.CodeDramaUploadAborted, "上传会话已取消，请重新上传")
	}
	return nil, cause
}

func (s *sAsset) finalizeDramaUpload(ctx context.Context, up *dao.DramaImportUpload, packKey, planJSON string) (*v1.CompleteDramaUploadRes, error) {
	job, created, err := dao.FinalizeDramaUploadOnDB(s.uploadDB(), ctx, up.Pk, packKey, planJSON)
	if errors.Is(err, dao.ErrDramaUploadAborted) {
		if job == nil || job.Status == dao.DramaJobCanceled {
			_, _ = s.purgeDramaUploadObjects(ctx, up)
		}
		return nil, gerror.NewCode(errcode.CodeDramaUploadAborted, "上传会话已取消，请重新上传")
	}
	if errors.Is(err, dao.ErrDramaUploadNotReady) {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "上传会话状态不允许完成")
	}
	if err != nil {
		return nil, gerror.Wrap(err, "创建导入任务失败")
	}
	if created {
		// 仅建任务的一方清理分片，pack 留给任务使用
		if store := s.store; store != nil {
			for _, p := range up.Parts {
				if err := store.RemoveObject(ctx, up.Bucket, p.ObjectKey); err != nil {
					g.Log().Warningf(ctx, "cleanup part %s: %v", p.ObjectKey, err)
				}
			}
		}
	}
	return &v1.CompleteDramaUploadRes{UploadId: up.Code, JobId: job.Code, Status: job.Status}, nil
}

// purgeDramaUploadObjects 仅在取消资格已提交后清理本会话前缀，脱离请求取消。
func (s *sAsset) purgeDramaUploadObjects(ctx context.Context, up *dao.DramaImportUpload) (int, error) {
	store := s.store
	if store == nil || up == nil || !strings.HasPrefix(up.Prefix, consts.DramaImportPrefix(up.Code)) {
		return 0, nil
	}
	cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), dramaPurgeTimeout)
	defer cancel()
	return store.RemovePrefix(cctx, up.Bucket, up.Prefix)
}

func (s *sAsset) AbortDramaUpload(ctx context.Context, id string) (*v1.AbortDramaUploadRes, error) {
	up, err := s.loadDramaUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := dao.AbortDramaUploadTxOnDB(s.uploadDB(), ctx, up.Pk, "上传会话已取消"); err != nil {
		if errors.Is(err, dao.ErrDramaUploadJobStarted) {
			return nil, gerror.NewCode(errcode.CodeDramaImportJobStarted, "导入任务已开始，不能取消上传会话")
		}
		return nil, err
	}
	deleted, err := s.purgeDramaUploadObjects(ctx, up)
	if err != nil {
		return nil, gerror.Wrap(err, "上传会话已取消，但清理临时对象失败，请重试取消")
	}
	return &v1.AbortDramaUploadRes{Id: up.Code, Deleted: deleted}, nil
}

// EnqueueDramaZipFile 兼容小包整包 POST：落到本会话 pack 后创建同一异步任务。
func (s *sAsset) EnqueueDramaZipFile(ctx context.Context, zipPath, zipName, planJSON, targetID string) (*v1.CompleteDramaUploadRes, error) {
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "无法读取压缩包")
	}
	lim := dramaImportLimits(ctx)
	if st.Size() > lim.MaxZipBytes {
		return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("压缩包大小 %s 超过上限 %s", humanBytes(st.Size()), humanBytes(lim.MaxZipBytes)))
	}
	if st.Size() > lim.MaxBodyBytes {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "超过单次请求上限，请使用分片上传")
	}
	upRes, err := s.CreateDramaUpload(ctx, zipName, st.Size())
	if err != nil {
		return nil, err
	}
	up, err := s.loadDramaUpload(ctx, upRes.Id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(zipPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	packKey := up.Prefix + "pack.zip"
	if err := s.store.PutObject(ctx, up.Bucket, packKey, "application/zip", f, st.Size()); err != nil {
		return nil, gerror.Wrap(err, "写入整包失败")
	}
	planJSON, err = mergeDramaPlanJSON(planJSON, targetID)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}
	return s.finalizeDramaUpload(ctx, up, packKey, planJSON)
}
