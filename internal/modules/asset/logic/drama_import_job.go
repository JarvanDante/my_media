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
)

const dramaJobStale = 5 * time.Minute

type dramaJobResult struct {
	Imported    int                   `json:"imported"`
	FailedCount int                   `json:"failed_count"`
	List        []v1.ImportDramasItem `json:"list"`
	Failed      []v1.ImportDramasFail `json:"failed"`
}

func (s *sAsset) GetDramaImportJob(ctx context.Context, id string) (*v1.DramaImportJobRes, error) {
	job, err := dao.GetDramaImportJobByCode(ctx, id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "导入任务不存在")
	}
	return s.dramaImportJobRes(ctx, job)
}

func (s *sAsset) dramaImportJobRes(ctx context.Context, job *dao.DramaImportJob) (*v1.DramaImportJobRes, error) {
	res := &v1.DramaImportJobRes{
		Id: job.Code, UploadId: job.UploadCode, Status: job.Status, Stage: job.Stage,
		Progress: job.Progress, Message: job.Message, Error: job.Error,
		Attempts: job.Attempts, MaxAttempts: job.MaxAttempts,
		List: make([]v1.DramaImportJobDrama, 0), Failed: make([]v1.ImportDramasFail, 0),
		CreatedAt: job.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: job.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if job.FinishedAt != nil {
		res.FinishedAt = job.FinishedAt.UTC().Format(time.RFC3339)
	}
	var result dramaJobResult
	if strings.TrimSpace(job.ResultJSON) != "" {
		_ = json.Unmarshal([]byte(job.ResultJSON), &result)
	}
	res.Imported = result.Imported
	res.FailedCount = result.FailedCount
	res.Failed = result.Failed
	if res.Failed == nil {
		res.Failed = []v1.ImportDramasFail{}
	}
	epTotal, epReady, epProc, epFail := 0, 0, 0, 0
	for _, it := range result.List {
		row := v1.DramaImportJobDrama{
			Id: it.Id, Title: it.Title, ChapterCount: it.ChapterCount,
			Appended: it.Appended, SkippedCount: it.SkippedCount, ImportedCount: it.ImportedCount,
		}
		if a, err := s.repo.GetByCode(ctx, it.Id); err == nil && a != nil {
			c, err := dao.CountEpisodesByParent(ctx, a.Pk)
			if err == nil {
				row.ChapterCount = c.Total
				row.ReadyCount = c.Ready
				row.ProcessingCount = c.Processing
				row.FailedCount = c.Failed
				epTotal += c.Total
				epReady += c.Ready
				epProc += c.Processing
				epFail += c.Failed
			}
		}
		res.List = append(res.List, row)
	}
	res.EpisodeTotal = epTotal
	res.EpisodeReady = epReady
	res.EpisodeProcessing = epProc
	res.EpisodeFailed = epFail
	return res, nil
}

func (s *sAsset) RetryDramaImportJob(ctx context.Context, id string) (*v1.DramaImportJobRes, error) {
	job, err := dao.GetDramaImportJobByCode(ctx, id)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "导入任务不存在")
	}
	switch job.Status {
	case dao.DramaJobSucceeded:
		return s.dramaImportJobRes(ctx, job)
	case dao.DramaJobRunning:
		return s.dramaImportJobRes(ctx, job)
	case dao.DramaJobQueued:
		return s.dramaImportJobRes(ctx, job)
	case dao.DramaJobImportDone:
		if err := s.retryDramaTranscode(ctx, job); err != nil {
			return nil, err
		}
		job, err = dao.GetDramaImportJobByCode(ctx, id)
		if err != nil {
			return nil, err
		}
		return s.dramaImportJobRes(ctx, job)
	case dao.DramaJobFailed, dao.DramaJobCanceled:
		if job.PackKey == "" || s.store == nil {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "整包已不可用，请重新上传")
		}
		if err := s.store.Stat(ctx, job.Bucket, job.PackKey); err != nil {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "整包已不可用，请重新上传")
		}
		if err := dao.RequeueDramaImportJob(ctx, job.Pk); err != nil {
			return nil, err
		}
		job, err = dao.GetDramaImportJobByCode(ctx, id)
		if err != nil {
			return nil, err
		}
		return s.dramaImportJobRes(ctx, job)
	default:
		return nil, gerror.NewCode(errcode.CodeBadRequest, "当前状态不能重试")
	}
}

func (s *sAsset) retryDramaTranscode(ctx context.Context, job *dao.DramaImportJob) error {
	var result dramaJobResult
	if strings.TrimSpace(job.ResultJSON) != "" {
		_ = json.Unmarshal([]byte(job.ResultJSON), &result)
	}
	var errs []string
	for _, it := range result.List {
		a, err := s.repo.GetByCode(ctx, it.Id)
		if err != nil || a == nil || !consts.IsDramaSeries(a.Kind, a.ParentId) {
			continue
		}
		eps, err := s.repo.ListEpisodes(ctx, a.Pk)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		for _, ep := range eps {
			if ep.Status == consts.AssetStatusReady {
				continue
			}
			if ep.SourceBucket == "" || ep.SourceKey == "" {
				continue
			}
			if _, err := s.TriggerTranscode(ctx, ep.Code, 8); err != nil {
				errs = append(errs, fmt.Sprintf("%s 第%d集: %s", it.Title, ep.Seq, err.Error()))
			}
		}
	}
	msg := job.Message
	if len(errs) > 0 {
		msg = "转码补投部分失败：" + strings.Join(errs, "；")
		if err := dao.UpdateDramaImportJobResult(ctx, job.Pk, dao.DramaJobImportDone, "transcoding", msg, job.ResultJSON, 100); err != nil {
			return err
		}
		return gerror.NewCode(errcode.CodeBadRequest, msg)
	}
	return dao.UpdateDramaImportJobResult(ctx, job.Pk, dao.DramaJobImportDone, "transcoding", "已补投转码任务", job.ResultJSON, 100)
}

func (s *sAsset) RunDramaImportWorker(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.tickDramaImport(ctx); err != nil {
				g.Log().Warningf(ctx, "drama import worker: %v", err)
			}
		}
	}
}

func (s *sAsset) tickDramaImport(ctx context.Context) error {
	s.cleanupExpiredDramaUploads(ctx)
	if err := dao.FailExhaustedStaleDramaJobs(ctx, dramaJobStale); err != nil {
		g.Log().Warningf(ctx, "fail exhausted drama jobs: %v", err)
	}
	s.refreshImportDoneJobs(ctx)
	lim := dramaImportLimits(ctx)
	running, err := dao.CountRunningDramaJobs(ctx, dramaJobStale)
	if err != nil {
		return err
	}
	if running >= lim.MaxRunningJobs {
		return nil
	}
	job, err := dao.ClaimDramaImportJob(ctx, dramaJobStale)
	if err != nil {
		return err
	}
	if job == nil {
		return nil
	}
	s.runDramaImportJob(ctx, job, lim)
	return nil
}

func (s *sAsset) cleanupExpiredDramaUploads(ctx context.Context) {
	list, err := dao.ListExpiredDramaUploads(ctx, 20)
	if err != nil {
		g.Log().Warningf(ctx, "list expired drama uploads: %v", err)
		return
	}
	for _, up := range list {
		if s.store != nil && strings.HasPrefix(up.Prefix, consts.PrefixDrama+"import/"+up.Code+"/") {
			if _, err := s.store.RemovePrefix(ctx, up.Bucket, up.Prefix); err != nil {
				g.Log().Warningf(ctx, "cleanup expired upload %s: %v", up.Code, err)
				continue
			}
		}
		if err := dao.AbortDramaUpload(ctx, up.Pk); err != nil {
			g.Log().Warningf(ctx, "abort expired upload %s: %v", up.Code, err)
		}
	}
}

func (s *sAsset) refreshImportDoneJobs(ctx context.Context) {
	list, err := dao.ListImportDoneDramaJobs(ctx, 50)
	if err != nil {
		g.Log().Warningf(ctx, "list import_done jobs: %v", err)
		return
	}
	for i := range list {
		job := list[i]
		res, err := s.dramaImportJobRes(ctx, &job)
		if err != nil || res.EpisodeTotal == 0 {
			continue
		}
		if res.EpisodeReady >= res.EpisodeTotal && res.EpisodeFailed == 0 && res.EpisodeProcessing == 0 {
			msg := fmt.Sprintf("全部 %d 集转码完成", res.EpisodeTotal)
			if err := dao.UpdateDramaImportJobResult(ctx, job.Pk, dao.DramaJobSucceeded, "succeeded", msg, job.ResultJSON, 100); err != nil {
				g.Log().Warningf(ctx, "mark job succeeded %s: %v", job.Code, err)
			}
		}
	}
}

func (s *sAsset) runDramaImportJob(parent context.Context, job *dao.DramaImportJob, lim DramaImportLimits) {
	timeout := time.Duration(lim.JobTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultJobTimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	_ = dao.HeartbeatDramaImportJob(ctx, job.Pk, "validating", 5, "正在校验整包")
	if s.store == nil {
		s.failDramaJob(ctx, job, "MinIO 未初始化", 5)
		return
	}
	if job.PackKey == "" || !strings.HasPrefix(job.PackKey, consts.PrefixDrama+"import/"+job.UploadCode+"/") {
		s.failDramaJob(ctx, job, "整包对象不属于本任务会话", 5)
		return
	}
	size, err := s.store.ObjectSize(ctx, job.Bucket, job.PackKey)
	if err != nil {
		s.failDramaJob(ctx, job, "整包不存在或无法读取", 5)
		return
	}
	if size > lim.MaxZipBytes {
		s.failDramaJob(ctx, job, fmt.Sprintf("压缩包大小 %s 超过上限 %s", humanBytes(size), humanBytes(lim.MaxZipBytes)), 5)
		return
	}
	if err := ensureDiskFree(os.TempDir(), lim.MinFreeBytes+size); err != nil {
		s.failDramaJob(ctx, job, err.Error(), 5)
		return
	}

	_ = dao.HeartbeatDramaImportJob(ctx, job.Pk, "extracting", 15, "正在下载整包")
	dir, err := os.MkdirTemp("", "drama-job-")
	if err != nil {
		s.failDramaJob(ctx, job, err.Error(), 15)
		return
	}
	defer os.RemoveAll(dir)
	packPath := filepath.Join(dir, "pack.zip")
	if err := s.downloadDramaPack(ctx, job.Bucket, job.PackKey, packPath, lim.MaxZipBytes); err != nil {
		s.failDramaJob(ctx, job, err.Error(), 15)
		return
	}

	_ = dao.HeartbeatDramaImportJob(ctx, job.Pk, "importing", 40, "正在解压入库")
	out, err := s.importDramasFromPack(ctx, packPath, "pack.zip", job.PlanJSON)
	result := dramaJobResult{}
	if out != nil {
		result.Imported = out.Imported
		result.FailedCount = out.FailedCount
		result.List = out.List
		result.Failed = out.Failed
	}
	raw, _ := json.Marshal(result)
	if err != nil && result.Imported == 0 {
		msg := err.Error()
		if ctxImportCanceled(ctx) != nil {
			msg = "超时/取消，结果可能部分完成，请刷新核对后再决定重试"
		}
		if ferr := dao.FinishDramaImportJob(parent, job.Pk, dao.DramaJobFailed, "failed", msg, msg, string(raw), 40); ferr != nil {
			g.Log().Warningf(parent, "finish drama job %s: %v (import err: %s)", job.Code, ferr, msg)
		}
		return
	}
	msg := "入库完成，转码进行中"
	if result.FailedCount > 0 {
		msg = "入库完成，部分失败或转码投递失败，可按步重试"
	}
	if ferr := dao.FinishDramaImportJob(parent, job.Pk, dao.DramaJobImportDone, "transcoding", msg, "", string(raw), 100); ferr != nil {
		g.Log().Warningf(parent, "finish drama job %s: %v", job.Code, ferr)
		return
	}
	// 入库完成后仅清理本任务临时整包，不删用户媒资
	if strings.HasPrefix(job.PackKey, consts.PrefixDrama+"import/"+job.UploadCode+"/") {
		if err := s.store.RemoveObject(parent, job.Bucket, job.PackKey); err != nil {
			g.Log().Warningf(parent, "cleanup pack %s: %v", job.PackKey, err)
		}
		if _, err := s.store.RemovePrefix(parent, job.Bucket, consts.DramaImportPrefix(job.UploadCode)); err != nil {
			g.Log().Warningf(parent, "cleanup import prefix %s: %v", job.UploadCode, err)
		}
	}
}

func (s *sAsset) failDramaJob(ctx context.Context, job *dao.DramaImportJob, msg string, progress int) {
	if ctxImportCanceled(ctx) != nil {
		msg = "超时/取消，结果可能部分完成，请刷新核对后再决定重试"
	}
	if err := dao.FinishDramaImportJob(context.Background(), job.Pk, dao.DramaJobFailed, "failed", msg, msg, job.ResultJSON, progress); err != nil {
		g.Log().Warningf(ctx, "fail drama job %s: %v (reason: %s)", job.Code, err, msg)
	}
}

func (s *sAsset) downloadDramaPack(ctx context.Context, bucket, key, dest string, max int64) error {
	rc, size, err := s.store.GetObject(ctx, bucket, key)
	if err != nil {
		return fmt.Errorf("下载整包失败: %w", err)
	}
	defer rc.Close()
	if size > max {
		return fmt.Errorf("压缩包大小 %s 超过上限 %s", humanBytes(size), humanBytes(max))
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(rc, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("压缩包大小超过上限 %s", humanBytes(max))
	}
	return nil
}
