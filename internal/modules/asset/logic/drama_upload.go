package logic

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
	"github.com/JarvanDante/my_media/internal/shared/storage"
)

const dramaUploadExpire = 24 * time.Hour

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
	up, err := dao.GetDramaUploadByCode(ctx, id)
	if err != nil {
		return nil, err
	}
	if up == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "上传会话不存在")
	}
	return up, nil
}

func (s *sAsset) refreshDramaUploadParts(ctx context.Context, up *dao.DramaImportUpload) error {
	if s.store == nil || up == nil {
		return nil
	}
	var newly []int
	for i, p := range up.Parts {
		if p.UploadedAt != nil {
			continue
		}
		sz, err := s.store.ObjectSize(ctx, up.Bucket, p.ObjectKey)
		if err != nil || sz != p.Size {
			continue
		}
		now := time.Now()
		up.Parts[i].UploadedAt = &now
		newly = append(newly, p.N)
	}
	return dao.MarkDramaPartsUploaded(ctx, up.Pk, newly)
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
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	up, err := s.loadDramaUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if up.Status == dao.DramaUploadAborted {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "上传会话已取消")
	}
	planJSON, err = mergeDramaPlanJSON(planJSON, targetID)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}
	if up.JobCode != "" {
		job, err := dao.GetDramaImportJobByCode(ctx, up.JobCode)
		if err != nil {
			return nil, err
		}
		if job != nil {
			return &v1.CompleteDramaUploadRes{UploadId: up.Code, JobId: job.Code, Status: job.Status}, nil
		}
	}
	if time.Now().After(up.ExpiresAt) && up.Status != dao.DramaUploadCompleted {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "上传会话已过期")
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
	needCompose := true
	if sz, err := s.store.ObjectSize(ctx, up.Bucket, packKey); err == nil && sz == up.TotalSize {
		needCompose = false
	}
	if needCompose {
		keys := make([]string, 0, len(up.Parts))
		var got int64
		for _, p := range up.Parts {
			if !strings.HasPrefix(p.ObjectKey, up.Prefix) {
				return nil, gerror.NewCode(errcode.CodeBadRequest, "分片对象不属于本会话")
			}
			sz, err := s.store.ObjectSize(ctx, up.Bucket, p.ObjectKey)
			if err != nil {
				return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片 %d 尚未上传完整", p.N))
			}
			if sz != p.Size {
				return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片 %d 大小不匹配（期望 %s，实际 %s）", p.N, humanBytes(p.Size), humanBytes(sz)))
			}
			keys = append(keys, p.ObjectKey)
			got += sz
		}
		if got != up.TotalSize {
			return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("分片合计 %s 与会话总量 %s 不一致", humanBytes(got), humanBytes(up.TotalSize)))
		}
		if err := s.store.ComposeObjects(ctx, up.Bucket, packKey, keys); err != nil {
			return nil, gerror.Wrap(err, "合并分片失败")
		}
		if sz, err := s.store.ObjectSize(ctx, up.Bucket, packKey); err != nil || sz != up.TotalSize {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "合并后的整包大小校验失败")
		}
	}
	up.PackKey = packKey
	if err := dao.CompleteDramaUpload(ctx, up.Pk, packKey, planJSON, up.JobCode); err != nil {
		return nil, err
	}
	job, err := dao.CreateDramaImportJob(ctx, up, planJSON)
	if err != nil {
		return nil, gerror.Wrap(err, "创建导入任务失败")
	}
	// 合并成功后只清理本会话分片，保留 pack 给任务使用
	for _, p := range up.Parts {
		if err := s.store.RemoveObject(ctx, up.Bucket, p.ObjectKey); err != nil {
			g.Log().Warningf(ctx, "cleanup part %s: %v", p.ObjectKey, err)
		}
	}
	return &v1.CompleteDramaUploadRes{UploadId: up.Code, JobId: job.Code, Status: job.Status}, nil
}

func (s *sAsset) AbortDramaUpload(ctx context.Context, id string) (*v1.AbortDramaUploadRes, error) {
	up, err := s.loadDramaUpload(ctx, id)
	if err != nil {
		return nil, err
	}
	if up.JobCode != "" {
		job, err := dao.GetDramaImportJobByCode(ctx, up.JobCode)
		if err != nil {
			return nil, err
		}
		if job != nil && (job.Status == dao.DramaJobRunning || job.Status == dao.DramaJobImportDone || job.Status == dao.DramaJobSucceeded) {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "导入任务已开始，不能取消上传会话")
		}
	}
	deleted := 0
	if s.store != nil && strings.HasPrefix(up.Prefix, consts.PrefixDrama+"import/"+up.Code+"/") {
		n, err := s.store.RemovePrefix(ctx, up.Bucket, up.Prefix)
		if err != nil {
			return nil, gerror.Wrap(err, "清理临时对象失败")
		}
		deleted = n
	}
	if err := dao.AbortDramaUpload(ctx, up.Pk); err != nil {
		return nil, err
	}
	if up.JobCode != "" {
		if job, _ := dao.GetDramaImportJobByCode(ctx, up.JobCode); job != nil && job.Status == dao.DramaJobQueued {
			_ = dao.FinishDramaImportJob(ctx, job.Pk, dao.DramaJobCanceled, "canceled", "上传会话已取消", "", job.ResultJSON, job.Progress)
		}
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
	up.PackKey = packKey
	if err := dao.CompleteDramaUpload(ctx, up.Pk, packKey, planJSON, ""); err != nil {
		return nil, err
	}
	up.PackKey = packKey
	job, err := dao.CreateDramaImportJob(ctx, up, planJSON)
	if err != nil {
		return nil, gerror.Wrap(err, "创建导入任务失败")
	}
	return &v1.CompleteDramaUploadRes{UploadId: up.Code, JobId: job.Code, Status: job.Status}, nil
}
