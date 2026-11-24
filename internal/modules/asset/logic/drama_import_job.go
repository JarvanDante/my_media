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
	"sync/atomic"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
	"github.com/JarvanDante/my_media/internal/shared/kit"
)

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

func newDramaImportWorkerID() string {
	id, err := kit.NewPublicID()
	if err != nil || id == "" {
		id = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	s := fmt.Sprintf("%s-%d", id, os.Getpid())
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func dramaJobLeaseRenewInterval(ttl time.Duration) time.Duration {
	return dao.DramaJobLeaseCallTimeout(ttl)
}

// DramaImportWorkerShutdownWait 关闭时等待 in-flight 退出并 Release 的上限。
func DramaImportWorkerShutdownWait() time.Duration {
	d := 2*dao.DramaJobLeaseCallTimeout(dao.DefaultDramaJobLeaseTTL) + time.Second
	if d > 8*time.Second {
		return 8 * time.Second
	}
	if d < 2*time.Second {
		return 2 * time.Second
	}
	return d
}

func leaseCallContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = dao.DramaJobLeaseCallTimeout(dao.DefaultDramaJobLeaseTTL)
	}
	return context.WithTimeout(context.Background(), timeout)
}

func canCleanupDramaImportTemp(finishErr error, leaseLost bool) bool {
	return finishErr == nil && !leaseLost
}

type dramaLeaseOps struct {
	TTL         time.Duration
	CallTimeout time.Duration
	Renew       func(ctx context.Context, lease dao.DramaJobLease, ttl time.Duration) error
	Heartbeat   func(ctx context.Context, lease dao.DramaJobLease, stage string, progress int, message string) error
	Finish      func(ctx context.Context, lease dao.DramaJobLease, status, stage, message, errMsg, resultJSON string, progress int) error
	Release     func(ctx context.Context, lease dao.DramaJobLease) error
}

func defaultDramaLeaseOps() dramaLeaseOps {
	return dramaLeaseOps{
		TTL:         dao.DefaultDramaJobLeaseTTL,
		CallTimeout: dao.DramaJobLeaseCallTimeout(dao.DefaultDramaJobLeaseTTL),
		Renew:       dao.RenewDramaImportJobLease,
		Heartbeat:   dao.HeartbeatDramaImportJob,
		Finish:      dao.FinishDramaImportJob,
		Release:     dao.ReleaseDramaImportJobLease,
	}
}

func (ops *dramaLeaseOps) ensure() {
	if ops.TTL <= 0 {
		ops.TTL = dao.DefaultDramaJobLeaseTTL
	}
	if ops.CallTimeout <= 0 {
		ops.CallTimeout = dao.DramaJobLeaseCallTimeout(ops.TTL)
	}
	if ops.Renew == nil {
		ops.Renew = dao.RenewDramaImportJobLease
	}
	if ops.Release == nil {
		ops.Release = dao.ReleaseDramaImportJobLease
	}
}

func (ops dramaLeaseOps) bounded(fn func(ctx context.Context) error) error {
	ops.ensure()
	ctx, cancel := leaseCallContext(ops.CallTimeout)
	defer cancel()
	return fn(ctx)
}

// runWithDramaJobLease 按最近一次成功续租设置本地失效deadline，不依赖卡住的 Renew 返回。
// Renew/Release 调用有界；续租失败或本地到期立即取消 workCtx。
func runWithDramaJobLease(parent context.Context, lease dao.DramaJobLease, ops dramaLeaseOps, work func(ctx context.Context) error) (leaseLost bool, err error) {
	ops.ensure()
	workCtx, cancelWork := context.WithCancel(parent)
	defer cancelWork()
	var lost atomic.Bool
	var deadline atomic.Int64
	deadline.Store(time.Now().Add(ops.TTL).UnixNano())

	renewDone := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		for {
			remain := time.Until(time.Unix(0, deadline.Load()))
			if remain <= 0 {
				lost.Store(true)
				cancelWork()
				return
			}
			timer := time.NewTimer(remain)
			select {
			case <-workCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
	go func() {
		defer close(renewDone)
		t := time.NewTicker(dramaJobLeaseRenewInterval(ops.TTL))
		defer t.Stop()
		for {
			select {
			case <-workCtx.Done():
				return
			case <-t.C:
				rerr := ops.bounded(func(ctx context.Context) error {
					return ops.Renew(ctx, lease, ops.TTL)
				})
				if rerr != nil {
					lost.Store(true)
					cancelWork()
					return
				}
				deadline.Store(time.Now().Add(ops.TTL).UnixNano())
			}
		}
	}()
	waitBound := ops.CallTimeout + 100*time.Millisecond
	defer func() {
		cancelWork()
		waitCh(renewDone, waitBound)
		waitCh(watchDone, 100*time.Millisecond)
	}()
	err = work(workCtx)
	leaseLost = lost.Load() || errors.Is(err, dao.ErrDramaJobLeaseLost)
	return leaseLost, err
}

func waitCh(ch <-chan struct{}, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
	}
}

func (s *sAsset) RunDramaImportWorker(ctx context.Context) {
	owner := newDramaImportWorkerID()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := s.tickDramaImport(ctx, owner); err != nil {
				g.Log().Warningf(ctx, "drama import worker: %v", err)
			}
		}
	}
}

func (s *sAsset) tickDramaImport(ctx context.Context, owner string) error {
	s.cleanupExpiredDramaUploads(ctx)
	if err := dao.FailExhaustedStaleDramaJobs(ctx); err != nil {
		g.Log().Warningf(ctx, "fail exhausted drama jobs: %v", err)
	}
	s.refreshImportDoneJobs(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	lim := dramaImportLimits(ctx)
	job, lease, err := dao.ClaimDramaImportJob(ctx, owner, dao.DefaultDramaJobLeaseTTL, lim.MaxRunningJobs)
	if err != nil {
		return err
	}
	if job == nil || lease == nil {
		return nil
	}
	s.runDramaImportJob(ctx, job, *lease, lim)
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

func (s *sAsset) runDramaImportJob(parent context.Context, job *dao.DramaImportJob, lease dao.DramaJobLease, lim DramaImportLimits) {
	timeout := time.Duration(lim.JobTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultJobTimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	ops := defaultDramaLeaseOps()

	lost, err := runWithDramaJobLease(ctx, lease, ops, func(workCtx context.Context) error {
		return s.execDramaImportJob(workCtx, job, lease, ops, lim)
	})
	if lost {
		g.Log().Warningf(parent, "drama import job %s lost lease, skip finish/cleanup", job.Code)
		if rerr := ops.bounded(func(c context.Context) error { return ops.Release(c, lease) }); rerr != nil && !errors.Is(rerr, dao.ErrDramaJobLeaseLost) {
			g.Log().Warningf(parent, "release after lost lease %s: %v", job.Code, rerr)
		}
		return
	}
	if err != nil && parent.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if rerr := ops.bounded(func(c context.Context) error { return ops.Release(c, lease) }); rerr != nil && !errors.Is(rerr, dao.ErrDramaJobLeaseLost) {
			g.Log().Warningf(parent, "release drama job lease %s: %v", job.Code, rerr)
		}
		return
	}
	if err != nil {
		g.Log().Warningf(parent, "drama import job %s: %v", job.Code, err)
	}
}

func (s *sAsset) execDramaImportJob(ctx context.Context, job *dao.DramaImportJob, lease dao.DramaJobLease, ops dramaLeaseOps, lim DramaImportLimits) error {
	if err := ops.bounded(func(c context.Context) error {
		return ops.Heartbeat(c, lease, "validating", 5, "正在校验整包")
	}); err != nil {
		return err
	}
	if s.store == nil {
		return s.failDramaJob(ctx, lease, job, ops, "MinIO 未初始化", 5)
	}
	if job.PackKey == "" || !strings.HasPrefix(job.PackKey, consts.PrefixDrama+"import/"+job.UploadCode+"/") {
		return s.failDramaJob(ctx, lease, job, ops, "整包对象不属于本任务会话", 5)
	}
	size, err := s.store.ObjectSize(ctx, job.Bucket, job.PackKey)
	if err != nil {
		return s.failDramaJob(ctx, lease, job, ops, "整包不存在或无法读取", 5)
	}
	if size > lim.MaxZipBytes {
		return s.failDramaJob(ctx, lease, job, ops, fmt.Sprintf("压缩包大小 %s 超过上限 %s", humanBytes(size), humanBytes(lim.MaxZipBytes)), 5)
	}
	if err := ensureDiskFree(os.TempDir(), lim.MinFreeBytes+size); err != nil {
		return s.failDramaJob(ctx, lease, job, ops, err.Error(), 5)
	}
	if err := ops.bounded(func(c context.Context) error {
		return ops.Heartbeat(c, lease, "extracting", 15, "正在下载整包")
	}); err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "drama-job-")
	if err != nil {
		return s.failDramaJob(ctx, lease, job, ops, err.Error(), 15)
	}
	defer os.RemoveAll(dir)
	packPath := filepath.Join(dir, "pack.zip")
	if err := s.downloadDramaPack(ctx, job.Bucket, job.PackKey, packPath, lim.MaxZipBytes); err != nil {
		return s.failDramaJob(ctx, lease, job, ops, err.Error(), 15)
	}
	if err := ops.bounded(func(c context.Context) error {
		return ops.Heartbeat(c, lease, "importing", 40, "正在解压入库")
	}); err != nil {
		return err
	}
	out, err := s.importDramasFromPack(ctx, packPath, "pack.zip", job.PlanJSON, lease)
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
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		if ctxImportCanceled(ctx) != nil {
			msg = "超时/取消，结果可能部分完成，请刷新核对后再决定重试"
		}
		ferr := ops.bounded(func(c context.Context) error {
			return ops.Finish(c, lease, dao.DramaJobFailed, "failed", msg, msg, string(raw), 40)
		})
		if !canCleanupDramaImportTemp(ferr, errors.Is(ferr, dao.ErrDramaJobLeaseLost)) {
			if ferr != nil {
				g.Log().Warningf(ctx, "finish drama job %s: %v (import err: %s)", job.Code, ferr, msg)
			}
			return ferr
		}
		return nil
	}
	msg := "入库完成，转码进行中"
	if result.FailedCount > 0 {
		msg = "入库完成，部分失败或转码投递失败，可按步重试"
	}
	ferr := ops.bounded(func(c context.Context) error {
		return ops.Finish(c, lease, dao.DramaJobImportDone, "transcoding", msg, "", string(raw), 100)
	})
	if !canCleanupDramaImportTemp(ferr, errors.Is(ferr, dao.ErrDramaJobLeaseLost)) {
		if ferr != nil {
			g.Log().Warningf(ctx, "finish drama job %s: %v", job.Code, ferr)
		}
		return ferr
	}
	if strings.HasPrefix(job.PackKey, consts.PrefixDrama+"import/"+job.UploadCode+"/") {
		cctx, ccancel := leaseCallContext(ops.CallTimeout)
		if err := s.store.RemoveObject(cctx, job.Bucket, job.PackKey); err != nil {
			g.Log().Warningf(ctx, "cleanup pack %s: %v", job.PackKey, err)
		}
		if _, err := s.store.RemovePrefix(cctx, job.Bucket, consts.DramaImportPrefix(job.UploadCode)); err != nil {
			g.Log().Warningf(ctx, "cleanup import prefix %s: %v", job.UploadCode, err)
		}
		ccancel()
	}
	return nil
}

func (s *sAsset) failDramaJob(ctx context.Context, lease dao.DramaJobLease, job *dao.DramaImportJob, ops dramaLeaseOps, msg string, progress int) error {
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ctx.Err()
	}
	if ctxImportCanceled(ctx) != nil {
		msg = "超时/取消，结果可能部分完成，请刷新核对后再决定重试"
	}
	ferr := ops.bounded(func(c context.Context) error {
		return ops.Finish(c, lease, dao.DramaJobFailed, "failed", msg, msg, job.ResultJSON, progress)
	})
	if errors.Is(ferr, dao.ErrDramaJobLeaseLost) {
		return ferr
	}
	if ferr != nil {
		g.Log().Warningf(ctx, "fail drama job %s: %v (reason: %s)", job.Code, ferr, msg)
	}
	return ferr
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
	n, err := copyWithContext(ctx, f, io.LimitReader(rc, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return fmt.Errorf("压缩包大小超过上限 %s", humanBytes(max))
	}
	return nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 32*1024)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return written, nil
			}
			return written, er
		}
	}
}
