package logic

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/gogf/gf/v2/frame/g"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
)

// dramaObjectStore 短剧导入路径用到的对象存储能力；*storage.Minio 满足此接口。
type dramaObjectStore interface {
	Bucket() string
	PublicURL(bucket, key string) string
	PutObject(ctx context.Context, bucket, key, contentType string, body io.Reader, size int64) error
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, int64, error)
	Stat(ctx context.Context, bucket, key string) error
	RemoveObject(ctx context.Context, bucket, key string) error
	RemovePrefix(ctx context.Context, bucket, prefix string) (int, error)
	ObjectSize(ctx context.Context, bucket, key string) (int64, error)
}

func (s *sAsset) dramaBlobs() dramaObjectStore {
	if s != nil && s.objects != nil {
		return s.objects
	}
	if s != nil && s.store != nil {
		return s.store
	}
	return nil
}

type dramaJobPlanSnap struct {
	ItemKey string `json:"item_key"`
	Title   string `json:"title"`
	Cover   bool   `json:"cover"`
	Seqs    []int  `json:"seqs"`
}

func planSnapsFromGroups(groups []DramaZipGroup) []dramaJobPlanSnap {
	out := make([]dramaJobPlanSnap, 0, len(groups))
	for _, g := range groups {
		seqs := make([]int, 0, len(g.Videos))
		for _, v := range g.Videos {
			if v.Seq > 0 {
				seqs = append(seqs, v.Seq)
			}
		}
		out = append(out, dramaJobPlanSnap{
			ItemKey: g.ItemKey,
			Title:   g.Title,
			Cover:   strings.TrimSpace(g.CoverPath) != "",
			Seqs:    seqs,
		})
	}
	return out
}

func parseDramaJobResult(raw string) dramaJobResult {
	var r dramaJobResult
	if strings.TrimSpace(raw) == "" {
		return r
	}
	_ = json.Unmarshal([]byte(raw), &r)
	return r
}

func parseDramaImportPlan(raw string) DramaImportPlan {
	var p DramaImportPlan
	if strings.TrimSpace(raw) == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

type dramaImportAssessment struct {
	ImportComplete bool
	Imported       int
	FailedCount    int
	Planned        int
	EpisodeTotal   int
	EpisodeReady   int
	EpisodeProc    int
	EpisodeFail    int
	Missing        []v1.ImportDramasFail
	Reason         string
}

func snapForAssessment(result dramaJobResult, planJSON string, groups []DramaZipGroup) []dramaJobPlanSnap {
	if len(result.Planned) > 0 {
		return result.Planned
	}
	if len(groups) > 0 {
		return planSnapsFromGroups(groups)
	}
	// 旧任务没有检查点：用结果列表项占位，计划项多出的只能记为缺失。
	out := make([]dramaJobPlanSnap, 0, len(result.List))
	for _, it := range result.List {
		out = append(out, dramaJobPlanSnap{ItemKey: it.ItemKey, Title: it.Title})
	}
	plan := parseDramaImportPlan(planJSON)
	if len(plan.Items) > len(out) {
		for i := len(out); i < len(plan.Items); i++ {
			out = append(out, dramaJobPlanSnap{Title: plan.Items[i].Title, Cover: strings.TrimSpace(plan.Items[i].Cover) != ""})
		}
	}
	return out
}

func (s *sAsset) assessDramaImport(ctx context.Context, job *dao.DramaImportJob, result dramaJobResult, groups []DramaZipGroup) dramaImportAssessment {
	a := dramaImportAssessment{}
	snaps := snapForAssessment(result, "", groups)
	if job != nil {
		snaps = snapForAssessment(result, job.PlanJSON, groups)
	}
	a.Planned = len(snaps)
	if a.Planned == 0 && len(result.List) == 0 {
		a.Reason = "没有可核对的导入计划项"
		a.FailedCount = result.FailedCount
		return a
	}

	listByKey := map[string]v1.ImportDramasItem{}
	listByID := map[string]v1.ImportDramasItem{}
	for _, it := range result.List {
		if it.ItemKey != "" {
			listByKey[it.ItemKey] = it
		}
		if it.Id != "" {
			listByID[it.Id] = it
		}
	}
	bindings := map[string]string{}
	if job != nil && job.Pk > 0 {
		if items, err := s.listJobBindings(ctx, job.Pk); err == nil {
			for _, b := range items {
				bindings[b.ItemKey] = b.DramaCode
			}
		}
	}

	for _, snap := range snaps {
		fail := func(msg string) {
			a.FailedCount++
			key := snap.ItemKey
			a.Missing = append(a.Missing, v1.ImportDramasFail{Title: snap.Title, Error: msg, ItemKey: key})
			a.EpisodeTotal += len(snap.Seqs)
		}
		code := ""
		if snap.ItemKey != "" {
			if it, ok := listByKey[snap.ItemKey]; ok {
				code = it.Id
			}
			if code == "" {
				code = bindings[snap.ItemKey]
			}
		}
		if code == "" && snap.ItemKey == "" {
			// 旧任务无 item_key：若计划项多于列表，多出的已在 snap 中无 id。
			for _, it := range result.List {
				if it.Title == snap.Title && listByID[it.Id].Id != "" {
					code = it.Id
					delete(listByID, it.Id)
					break
				}
			}
		}
		if code == "" {
			fail("未入库或导入失败")
			continue
		}
		series, err := s.repo.GetByCode(ctx, code)
		if err != nil || series == nil || !consts.IsDramaSeries(series.Kind, series.ParentId) {
			fail("已绑定的短剧不存在或已被删除")
			continue
		}
		eps, err := s.repo.ListEpisodes(ctx, series.Pk)
		if err != nil {
			fail("读取剧集失败: " + err.Error())
			continue
		}
		bySeq := map[int]domain.Asset{}
		for _, ep := range eps {
			bySeq[ep.Seq] = ep
			a.EpisodeTotal++
			switch ep.Status {
			case consts.AssetStatusReady:
				a.EpisodeReady++
			case consts.AssetStatusProcessing:
				a.EpisodeProc++
			case consts.AssetStatusFailed:
				a.EpisodeFail++
			}
		}
		// 计划集数未入库的计入总数且不算就绪。
		for _, seq := range snap.Seqs {
			if _, ok := bySeq[seq]; ok {
				continue
			}
			a.EpisodeTotal++
		}
		itemOK := true
		var reasons []string
		for _, seq := range snap.Seqs {
			ep, ok := bySeq[seq]
			if !ok {
				itemOK = false
				reasons = append(reasons, fmt.Sprintf("缺第%d集", seq))
				continue
			}
			if episodeImportSatisfied(ep) {
				continue
			}
			itemOK = false
			reasons = append(reasons, fmt.Sprintf("第%d集未完成入库", seq))
		}
		if snap.Cover && strings.TrimSpace(series.CoverUrl) == "" {
			itemOK = false
			reasons = append(reasons, "封面未入库")
		}
		if !itemOK {
			fail(strings.Join(reasons, "；"))
			continue
		}
		a.Imported++
	}

	// 转码投递失败等：结果 Failed 里可能还有完整项的告警，不把它算成导入未完成，但要保留失败数展示。
	importFail := a.FailedCount
	if result.FailedCount > importFail {
		a.FailedCount = result.FailedCount
	}
	if importFail == 0 && a.Imported == a.Planned && a.Planned > 0 {
		a.ImportComplete = true
	} else if importFail == 0 && a.Planned == 0 && len(result.List) > 0 && result.FailedCount == 0 {
		// 历史无计划快照且无失败记录：仅能按已列项视为导入完成（不能证明计划被全部覆盖）。
		a.ImportComplete = true
		a.Imported = len(result.List)
		a.Reason = "历史任务无绑定检查点，仅按已入库列表判断；不能证明原计划已全部覆盖"
	}
	if !a.ImportComplete && a.Reason == "" {
		a.Reason = fmt.Sprintf("导入未完成：计划 %d 项，已完整入库 %d 项，未完成 %d 项", a.Planned, a.Imported, importFail)
	}
	return a
}

// episodeImportSatisfied 已有源且处于就绪/转码中，或已就绪，视为该集入库完成（明确跳过已有）。
func episodeImportSatisfied(ep domain.Asset) bool {
	if ep.Status == consts.AssetStatusReady {
		return true
	}
	if ep.SourceKey != "" && (ep.Status == consts.AssetStatusProcessing || ep.Status == consts.AssetStatusDraft || ep.Status == consts.AssetStatusFailed) {
		// draft/failed 但已有源：原片已在对象存储，不必再依赖 zip；转码失败可只补投。
		// 但 draft/failed 且本次计划仍要确保源在：算入库完成（源已绑定）。
		return true
	}
	return false
}

func (s *sAsset) listJobBindings(ctx context.Context, jobPK int64) ([]dao.DramaImportItemBinding, error) {
	db := g.DB()
	if s != nil && s.jobDB != nil {
		db = s.jobDB
	}
	return dao.ListDramaImportItemBindingsOnDB(db, ctx, jobPK)
}

func (s *sAsset) getImportJobByCode(ctx context.Context, code string) (*dao.DramaImportJob, error) {
	if s != nil && s.jobDB != nil {
		return dao.GetDramaImportJobByCodeOnDB(s.jobDB, ctx, code)
	}
	return dao.GetDramaImportJobByCode(ctx, code)
}

func (s *sAsset) getImportJobByPK(ctx context.Context, id int64) (*dao.DramaImportJob, error) {
	if s != nil && s.jobDB != nil {
		return dao.GetDramaImportJobByPKOnDB(s.jobDB, ctx, id)
	}
	return dao.GetDramaImportJobByPKOnDB(g.DB(), ctx, id)
}

func (s *sAsset) updateImportJobResult(ctx context.Context, id int64, status, stage, message, resultJSON string, progress int) error {
	if s != nil && s.jobDB != nil {
		return dao.UpdateDramaImportJobResultOnDB(s.jobDB, ctx, id, status, stage, message, resultJSON, progress)
	}
	return dao.UpdateDramaImportJobResult(ctx, id, status, stage, message, resultJSON, progress)
}

func (s *sAsset) listImportDoneJobs(ctx context.Context, limit int) ([]dao.DramaImportJob, error) {
	if s != nil && s.jobDB != nil {
		return dao.ListImportDoneDramaJobsOnDB(s.jobDB, ctx, limit)
	}
	return dao.ListImportDoneDramaJobs(ctx, limit)
}

func (s *sAsset) requeueImportJob(ctx context.Context, id int64) error {
	if s != nil && s.jobDB != nil {
		return dao.RequeueDramaImportJobOnDB(s.jobDB, ctx, id)
	}
	return dao.RequeueDramaImportJob(ctx, id)
}

func (s *sAsset) countEpisodes(ctx context.Context, parentPK int64) (dao.EpisodeStatusCount, error) {
	if s != nil && s.jobDB != nil {
		return dao.CountEpisodesByParentOnDB(s.jobDB, ctx, parentPK)
	}
	return dao.CountEpisodesByParent(ctx, parentPK)
}

func (s *sAsset) resolveImportSeries(ctx context.Context, jobPK int64, itemKey, title, explicitTarget string) (*domain.Asset, bool, error) {
	if s != nil && s.jobDB != nil {
		return dao.ResolveDramaImportSeriesOnDB(s.jobDB, ctx, jobPK, itemKey, title, explicitTarget)
	}
	return dao.ResolveDramaImportSeries(ctx, jobPK, itemKey, title, explicitTarget)
}

func (s *sAsset) withImportLease(ctx context.Context, lease dao.DramaJobLease, fn func(ctx context.Context) error) error {
	if lease.JobPK <= 0 || strings.TrimSpace(lease.Owner) == "" || lease.Generation <= 0 {
		return fmt.Errorf("缺少有效的导入任务租约")
	}
	if s != nil && s.jobDB != nil {
		return dao.WithDramaJobLeaseOnDB(s.jobDB, ctx, lease, fn)
	}
	return dao.WithDramaJobLease(ctx, lease, fn)
}

func jobSourceOwned(job *dao.DramaImportJob) bool {
	if job == nil || job.PackKey == "" || job.UploadCode == "" {
		return false
	}
	prefix := consts.DramaImportPrefix(job.UploadCode)
	return strings.HasPrefix(job.PackKey, prefix)
}

func canCleanupDramaImportSource(importComplete bool, finishErr error, leaseLost bool) bool {
	return importComplete && finishErr == nil && !leaseLost
}

func mergeImportFails(base, extra []v1.ImportDramasFail) []v1.ImportDramasFail {
	out := append([]v1.ImportDramasFail{}, base...)
	seen := map[string]bool{}
	for _, f := range out {
		seen[f.ItemKey+"\x00"+f.Title+"\x00"+f.Error] = true
	}
	for _, f := range extra {
		k := f.ItemKey + "\x00" + f.Title + "\x00" + f.Error
		if seen[k] {
			continue
		}
		out = append(out, f)
		seen[k] = true
	}
	return out
}

func peekDramaPlanGroups(zipPath, zipName, planJSON string) ([]DramaZipGroup, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	metas, err := zipMetasFromReader(&zr.Reader)
	if err != nil {
		return nil, err
	}
	groups, err := GroupDramaZip(metas, zipName)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(planJSON) != "" {
		var raw DramaImportPlan
		if err := json.Unmarshal([]byte(planJSON), &raw); err != nil {
			return groups, nil
		}
		if len(raw.Items) > 0 {
			if planned, err := applyDramaPlan(groups, planJSON); err == nil {
				groups = planned
			}
		}
	}
	if err := ensureUniqueDramaItemKeys(groups); err != nil {
		return groups, nil
	}
	return groups, nil
}

func (s *sAsset) cleanupDramaImportSource(ctx context.Context, job *dao.DramaImportJob, ops dramaLeaseOps) {
	if job == nil || !jobSourceOwned(job) || s.dramaBlobs() == nil {
		return
	}
	ops.ensure()
	cctx, cancel := leaseCallContext(ops.CallTimeout)
	defer cancel()
	store := s.dramaBlobs()
	if err := store.RemoveObject(cctx, job.Bucket, job.PackKey); err != nil {
		g.Log().Warningf(ctx, "cleanup pack %s: %v", job.PackKey, err)
	}
	if _, err := store.RemovePrefix(cctx, job.Bucket, consts.DramaImportPrefix(job.UploadCode)); err != nil {
		g.Log().Warningf(ctx, "cleanup import prefix %s: %v", job.UploadCode, err)
	}
}
