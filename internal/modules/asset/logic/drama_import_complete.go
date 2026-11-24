package logic

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	"github.com/JarvanDante/my_media/internal/shared/errcode"

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
	ItemKey string   `json:"item_key"`
	Title   string   `json:"title"`
	Cover   bool     `json:"cover"`
	Seqs    []int    `json:"seqs"`
	Paths   []string `json:"paths,omitempty"` // 完整ZIP分组时记下的路径，用作稳定身份证据；不是计划本身
}

func planSnapsFromGroups(groups []DramaZipGroup) []dramaJobPlanSnap {
	out := make([]dramaJobPlanSnap, 0, len(groups))
	for _, g := range groups {
		seqs := make([]int, 0, len(g.Videos))
		paths := make([]string, 0, len(g.Videos)+1)
		for _, v := range g.Videos {
			if v.Seq > 0 {
				seqs = append(seqs, v.Seq)
			}
			if v.Path != "" {
				paths = append(paths, v.Path)
			}
		}
		if g.CoverPath != "" {
			paths = append(paths, g.CoverPath)
		}
		for _, c := range g.Covers {
			if c.Path != "" && c.Path != g.CoverPath {
				paths = append(paths, c.Path)
			}
		}
		out = append(out, dramaJobPlanSnap{
			ItemKey: g.ItemKey,
			Title:   g.Title,
			Cover:   g.CoverPath != "",
			Seqs:    seqs,
			Paths:   paths,
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

func uniquePositiveSeqs(seqs []int) ([]int, bool) {
	seen := map[int]bool{}
	out := make([]int, 0, len(seqs))
	for _, seq := range seqs {
		if seq < 1 {
			return nil, false
		}
		if seen[seq] {
			return nil, false
		}
		seen[seq] = true
		out = append(out, seq)
	}
	return out, true
}

type planItemObligation struct {
	Title     string
	Cover     bool
	CoverPath string
	Seqs      []int
	Paths     []string
}

func parsePlanItemObligation(i int, it DramaImportPlanItem) (planItemObligation, error) {
	ob := planItemObligation{Title: it.Title}
	var seqs []int
	for _, ep := range it.Episodes {
		if ep.Path == "" {
			return ob, fmt.Errorf("计划第%d项剧集路径为空", i+1)
		}
		p, err := sanitizeZipEntryName(ep.Path)
		if err != nil {
			return ob, fmt.Errorf("计划第%d项剧集路径无效: %w", i+1, err)
		}
		ob.Paths = append(ob.Paths, p)
		seqs = append(seqs, ep.Seq)
	}
	if it.Cover != "" {
		c, err := sanitizeZipEntryName(it.Cover)
		if err != nil {
			return ob, fmt.Errorf("计划第%d项封面路径无效: %w", i+1, err)
		}
		ob.Cover = true
		ob.CoverPath = c
		ob.Paths = append(ob.Paths, c)
	}
	if len(ob.Paths) == 0 {
		return ob, fmt.Errorf("计划第%d项缺少可用于映射目录身份的路径", i+1)
	}
	uniq, ok := uniquePositiveSeqs(seqs)
	if !ok || len(uniq) == 0 {
		return ob, fmt.Errorf("计划第%d项集号缺失、无效或重复，无法确定覆盖义务", i+1)
	}
	ob.Seqs = uniq
	return ob, nil
}

func parsePlanObligations(plan DramaImportPlan) ([]planItemObligation, error) {
	if len(plan.Items) == 0 {
		return nil, fmt.Errorf("导入计划没有项目")
	}
	out := make([]planItemObligation, 0, len(plan.Items))
	for i, it := range plan.Items {
		ob, err := parsePlanItemObligation(i, it)
		if err != nil {
			return nil, err
		}
		out = append(out, ob)
	}
	return out, nil
}

func seqSetEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int]int{}
	for _, n := range a {
		seen[n]++
	}
	for _, n := range b {
		if seen[n] == 0 {
			return false
		}
		seen[n]--
	}
	return true
}

func snapHasAllPaths(snap dramaJobPlanSnap, paths []string) bool {
	if len(paths) == 0 || len(snap.Paths) == 0 {
		return false
	}
	have := map[string]bool{}
	for _, p := range snap.Paths {
		have[p] = true
	}
	for _, p := range paths {
		if !have[p] {
			return false
		}
	}
	return true
}

// findGroupByPlanPaths 只用路径隶属完整 ZIP 分组，不用标题。
func findGroupByPlanPaths(groups []DramaZipGroup, ob planItemObligation) (DramaZipGroup, error) {
	var found *DramaZipGroup
	for _, p := range ob.Paths {
		if ob.Cover && p == ob.CoverPath {
			continue
		}
		var hit *DramaZipGroup
		for i := range groups {
			if inGroup(groups[i], p, false) {
				if hit != nil && hit.ItemKey != groups[i].ItemKey {
					return DramaZipGroup{}, fmt.Errorf("路径 %q 落在多个目录分组", p)
				}
				hit = &groups[i]
			}
		}
		if hit == nil {
			return DramaZipGroup{}, fmt.Errorf("计划路径 %q 不在完整压缩包分组中", p)
		}
		if found == nil {
			found = hit
		} else if found.ItemKey != hit.ItemKey {
			return DramaZipGroup{}, fmt.Errorf("同一计划项的路径跨越不同目录身份")
		}
	}
	if found == nil {
		return DramaZipGroup{}, fmt.Errorf("计划项缺少可用于定位分组的剧集路径")
	}
	if ob.Cover && !inGroup(*found, ob.CoverPath, true) {
		return DramaZipGroup{}, fmt.Errorf("封面不在对应目录分组内")
	}
	return *found, nil
}

func snapsFromPlanAndGroups(items []planItemObligation, groups []DramaZipGroup) ([]dramaJobPlanSnap, error) {
	seen := map[string]int{}
	out := make([]dramaJobPlanSnap, 0, len(items))
	for i, ob := range items {
		g, err := findGroupByPlanPaths(groups, ob)
		if err != nil {
			return nil, fmt.Errorf("计划第%d项无法映射到完整ZIP分组身份: %w", i+1, err)
		}
		key, err := dao.NormalizeDramaImportItemKey(g.ItemKey)
		if err != nil {
			return nil, fmt.Errorf("计划第%d项目录身份无效: %w", i+1, err)
		}
		if prev, ok := seen[key]; ok {
			return nil, fmt.Errorf("计划第%d项与第%d项映射到同一目录身份 %q，拒绝合并覆盖义务", i+1, prev+1, key)
		}
		seen[key] = i
		out = append(out, dramaJobPlanSnap{
			ItemKey: key,
			Title:   ob.Title,
			Cover:   ob.Cover,
			Seqs:    ob.Seqs,
			Paths:   append([]string{}, ob.Paths...),
		})
	}
	return out, nil
}

func planEpisodePaths(ob planItemObligation) []string {
	out := make([]string, 0, len(ob.Paths))
	for _, p := range ob.Paths {
		if ob.Cover && p == ob.CoverPath {
			continue
		}
		out = append(out, p)
	}
	return out
}

func mapPlanByCheckpointPaths(items []planItemObligation, snaps []dramaJobPlanSnap) ([]dramaJobPlanSnap, error) {
	used := map[int]bool{}
	out := make([]dramaJobPlanSnap, 0, len(items))
	seen := map[string]int{}
	for i, ob := range items {
		match := -1
		need := planEpisodePaths(ob)
		if len(need) == 0 {
			return nil, fmt.Errorf("计划第%d项缺少用于映射身份的剧集路径", i+1)
		}
		for j, snap := range snaps {
			if !snapHasAllPaths(snap, need) {
				continue
			}
			if match >= 0 {
				return nil, fmt.Errorf("计划第%d项路径同时匹配多个身份检查点，拒绝推断", i+1)
			}
			match = j
		}
		if match < 0 {
			return nil, fmt.Errorf("计划第%d项无法按路径映射到持久身份检查点", i+1)
		}
		if used[match] {
			return nil, fmt.Errorf("多个计划项映射到同一持久身份检查点，拒绝推断")
		}
		used[match] = true
		key, err := dao.NormalizeDramaImportItemKey(snaps[match].ItemKey)
		if err != nil {
			return nil, fmt.Errorf("检查点第%d项目录身份无效", match+1)
		}
		if prev, ok := seen[key]; ok {
			return nil, fmt.Errorf("计划第%d项与第%d项映射到同一目录身份 %q", i+1, prev+1, key)
		}
		seen[key] = i
		out = append(out, dramaJobPlanSnap{
			ItemKey: key,
			Title:   ob.Title,
			Cover:   ob.Cover,
			Seqs:    ob.Seqs,
			Paths:   append([]string{}, ob.Paths...),
		})
	}
	return out, nil
}

func mapPlanByCheckpointStructure(items []planItemObligation, snaps []dramaJobPlanSnap) ([]dramaJobPlanSnap, error) {
	if len(items) != len(snaps) {
		return nil, fmt.Errorf("持久身份检查点项数 %d 与原始计划项数 %d 不一致，拒绝按位置对齐身份", len(snaps), len(items))
	}
	seen := map[string]int{}
	out := make([]dramaJobPlanSnap, 0, len(items))
	for i, ob := range items {
		if !seqSetEqual(ob.Seqs, snaps[i].Seqs) || ob.Cover != snaps[i].Cover {
			return nil, fmt.Errorf("检查点第%d项的集号或封面义务与原始计划不一致，拒绝按位置对齐身份", i+1)
		}
		key, err := dao.NormalizeDramaImportItemKey(snaps[i].ItemKey)
		if err != nil {
			return nil, fmt.Errorf("检查点第%d项目录身份无效", i+1)
		}
		if prev, ok := seen[key]; ok {
			return nil, fmt.Errorf("计划第%d项与第%d项映射到同一目录身份 %q", i+1, prev+1, key)
		}
		seen[key] = i
		out = append(out, dramaJobPlanSnap{
			ItemKey: key,
			Title:   ob.Title,
			Cover:   ob.Cover,
			Seqs:    ob.Seqs,
			Paths:   append([]string{}, ob.Paths...),
		})
	}
	return out, nil
}

func mapPlanToIdentityCheckpoint(items []planItemObligation, snaps []dramaJobPlanSnap) ([]dramaJobPlanSnap, error) {
	if len(snaps) == 0 {
		return nil, fmt.Errorf("缺少持久身份检查点")
	}
	allHavePaths := true
	anyHavePaths := false
	for _, s := range snaps {
		if len(s.Paths) > 0 {
			anyHavePaths = true
		} else {
			allHavePaths = false
		}
	}
	if allHavePaths {
		return mapPlanByCheckpointPaths(items, snaps)
	}
	if !anyHavePaths {
		return mapPlanByCheckpointStructure(items, snaps)
	}
	return nil, fmt.Errorf("持久身份检查点路径证据不完整，拒绝推断目录身份")
}

// resolvePlanObligations 以不可变原计划为覆盖义务（项目、集号、封面），
// ItemKey 只来自完整 ZIP 初始分组或由此落下的持久身份检查点。
// 绝不按选中子集重算共同父来生成新身份，也不按标题匹配，不改写原计划。
func resolvePlanObligations(plan DramaImportPlan, groups []DramaZipGroup, result dramaJobResult) ([]dramaJobPlanSnap, error) {
	items, err := parsePlanObligations(plan)
	if err != nil {
		return nil, err
	}
	if len(groups) > 0 {
		return snapsFromPlanAndGroups(items, groups)
	}
	snaps, why := plannedCheckpointTrusted(result)
	if why != "" {
		return nil, fmt.Errorf("%s；无法将原始计划映射到完整ZIP分组或持久身份检查点，不能从选中路径重算目录身份，也不能从已入库列表反推", why)
	}
	mapped, err := mapPlanToIdentityCheckpoint(items, snaps)
	if err != nil {
		return nil, fmt.Errorf("%w；拒绝视为导入完成", err)
	}
	return mapped, nil
}

func plannedCheckpointTrusted(result dramaJobResult) ([]dramaJobPlanSnap, string) {
	if len(result.Planned) == 0 {
		return nil, "缺少持久计划检查点"
	}
	if result.PlannedCount > 0 && result.PlannedCount != len(result.Planned) {
		return nil, fmt.Sprintf("检查点项目数不一致：PlannedCount=%d，Planned=%d，拒绝从部分检查点推断完整性", result.PlannedCount, len(result.Planned))
	}
	seen := map[string]bool{}
	out := make([]dramaJobPlanSnap, 0, len(result.Planned))
	for i, snap := range result.Planned {
		key := strings.TrimSpace(snap.ItemKey)
		if key == "" {
			// item key 不得用 TrimSpace 当身份；这里只判断是否空。空键不可信。
			if snap.ItemKey == "" {
				return nil, fmt.Sprintf("检查点第%d项缺少稳定目录身份", i+1)
			}
			key = snap.ItemKey
		} else if key != snap.ItemKey {
			// 含首尾空白的 key 仍是合法身份，保留原值。
			key = snap.ItemKey
		}
		norm, err := dao.NormalizeDramaImportItemKey(snap.ItemKey)
		if err != nil {
			return nil, fmt.Sprintf("检查点第%d项目录身份无效", i+1)
		}
		if seen[norm] {
			return nil, fmt.Sprintf("检查点目录身份 %q 重复，拒绝推断完整性", norm)
		}
		seen[norm] = true
		uniq, ok := uniquePositiveSeqs(snap.Seqs)
		if !ok || len(uniq) == 0 {
			return nil, fmt.Sprintf("检查点第%d项缺少完整集号覆盖义务", i+1)
		}
		cp := snap
		cp.ItemKey = norm
		cp.Seqs = uniq
		out = append(out, cp)
	}
	return out, ""
}

// obligationSnaps 覆盖义务以原始计划为准；目录身份只来自完整 ZIP 分组或持久身份检查点。
// 绝不把已入库 result.List 当作计划本身，也不从选中子集重算 ItemKey。无法证明时 trusted=false。
func obligationSnaps(job *dao.DramaImportJob, result dramaJobResult, groups []DramaZipGroup) ([]dramaJobPlanSnap, bool, string) {
	var plan DramaImportPlan
	if job != nil {
		plan = parseDramaImportPlan(job.PlanJSON)
	}
	if len(plan.Items) > 0 {
		snaps, err := resolvePlanObligations(plan, groups, result)
		if err != nil {
			return nil, false, "原始导入计划无法核对其覆盖义务或稳定目录身份: " + err.Error()
		}
		return snaps, true, ""
	}
	if len(groups) > 0 {
		snaps, why := plannedCheckpointTrusted(dramaJobResult{Planned: planSnapsFromGroups(groups), PlannedCount: len(groups)})
		if why != "" {
			return nil, false, "当前压缩包分组无法形成完整覆盖义务: " + why
		}
		return snaps, true, ""
	}
	if snaps, why := plannedCheckpointTrusted(result); why == "" {
		return snaps, true, ""
	} else {
		return nil, false, why + "；缺少原始计划或完整检查点时，不能从已入库列表反推完整性"
	}
}

func (s *sAsset) assessDramaImport(ctx context.Context, job *dao.DramaImportJob, result dramaJobResult, groups []DramaZipGroup) dramaImportAssessment {
	a := dramaImportAssessment{}
	snaps, trusted, why := obligationSnaps(job, result, groups)
	a.Planned = len(snaps)
	if !trusted {
		a.Reason = why
		if a.Reason == "" {
			a.Reason = "缺少完整可信的导入检查点，不能证明计划已全部覆盖，拒绝视为导入完成"
		}
		a.FailedCount = result.FailedCount
		if a.FailedCount < 1 {
			a.FailedCount = 1
		}
		a.ImportComplete = false
		return a
	}

	listByKey := map[string]v1.ImportDramasItem{}
	for _, it := range result.List {
		if it.ItemKey != "" {
			if k, err := dao.NormalizeDramaImportItemKey(it.ItemKey); err == nil {
				listByKey[k] = it
			}
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
			a.Missing = append(a.Missing, v1.ImportDramasFail{Title: snap.Title, Error: msg, ItemKey: snap.ItemKey})
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
		// 只按稳定目录身份映射，不用标题从 List 反推。
		if code == "" {
			fail("未入库或导入失败")
			for range snap.Seqs {
				a.EpisodeTotal++
			}
			continue
		}
		series, err := s.repo.GetByCode(ctx, code)
		if err != nil || series == nil || !consts.IsDramaSeries(series.Kind, series.ParentId) {
			fail("已绑定的短剧不存在或已被删除")
			a.EpisodeTotal += len(snap.Seqs)
			continue
		}
		eps, err := s.repo.ListEpisodes(ctx, series.Pk)
		if err != nil {
			fail("读取剧集失败: " + err.Error())
			a.EpisodeTotal += len(snap.Seqs)
			continue
		}
		bySeq := map[int]domain.Asset{}
		for _, ep := range eps {
			bySeq[ep.Seq] = ep
		}
		itemOK := true
		var reasons []string
		for _, seq := range snap.Seqs {
			a.EpisodeTotal++
			ep, ok := bySeq[seq]
			if !ok {
				itemOK = false
				reasons = append(reasons, fmt.Sprintf("缺第%d集", seq))
				continue
			}
			switch ep.Status {
			case consts.AssetStatusReady:
				a.EpisodeReady++
			case consts.AssetStatusProcessing:
				a.EpisodeProc++
			case consts.AssetStatusFailed:
				a.EpisodeFail++
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

	importFail := a.FailedCount
	if result.FailedCount > a.FailedCount {
		a.FailedCount = result.FailedCount
	}

	transcodeWarned := 0
	for _, f := range result.Failed {
		key := f.ItemKey
		if key != "" {
			if k, err := dao.NormalizeDramaImportItemKey(key); err == nil {
				key = k
			}
		}
		complete := false
		if key != "" {
			for _, snap := range snaps {
				if snap.ItemKey == key {
					// 该项已计入 Imported 则视为入库完成仅转码告警
					// 通过再扫 Missing 判断
					hitMissing := false
					for _, m := range a.Missing {
						mk := m.ItemKey
						if mk != "" {
							if k, err := dao.NormalizeDramaImportItemKey(mk); err == nil {
								mk = k
							}
						}
						if mk == key {
							hitMissing = true
							break
						}
					}
					complete = !hitMissing && a.Imported > 0
					break
				}
			}
		}
		if complete {
			transcodeWarned++
		}
	}
	if importFail == 0 && a.Imported == a.Planned && a.Planned > 0 {
		if result.FailedCount > transcodeWarned && result.FailedCount > 0 {
			a.ImportComplete = false
			a.Reason = "结果失败数与已证明的计划覆盖义务矛盾，拒绝视为导入完成"
		} else {
			a.ImportComplete = true
		}
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

func (s *sAsset) requeueIncompleteImportDone(ctx context.Context, id int64) error {
	if s != nil && s.jobDB != nil {
		return dao.RequeueIncompleteImportDoneJobOnDB(s.jobDB, ctx, id)
	}
	return dao.RequeueIncompleteImportDoneJob(ctx, id)
}

func (s *sAsset) packReadyForRequeue(ctx context.Context, job *dao.DramaImportJob) error {
	if job == nil || job.PackKey == "" || s.dramaBlobs() == nil {
		return gerror.NewCode(errcode.CodeBadRequest, "整包已不可用，请重新上传")
	}
	if err := s.dramaBlobs().Stat(ctx, job.Bucket, job.PackKey); err != nil {
		return gerror.NewCode(errcode.CodeBadRequest, "整包已不可用，请重新上传")
	}
	return nil
}

func mapRequeueErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, dao.ErrDramaImportJobNotRequeued) {
		return gerror.NewCode(errcode.CodeBadRequest, "任务状态已变化，未能转入重试排队，请刷新后重试")
	}
	return err
}

func (s *sAsset) requeueIncompleteImportDoneIfPackReady(ctx context.Context, job *dao.DramaImportJob) error {
	if err := s.packReadyForRequeue(ctx, job); err != nil {
		return gerror.NewCode(errcode.CodeBadRequest, "入库未完成且整包已不可用，无法补齐失败项，请重新上传")
	}
	if err := s.requeueIncompleteImportDone(ctx, job.Pk); err != nil {
		return mapRequeueErr(err)
	}
	// 快照后、转入排队后源被删：不把仍成功排队的结果交给调用方。
	if err := s.packReadyForRequeue(ctx, job); err != nil {
		return gerror.NewCode(errcode.CodeBadRequest, "任务在转入重试排队后整包已不可用，请重新上传，不要当作已成功排队")
	}
	return nil
}

func (s *sAsset) requeueFailedOrCanceledIfPackReady(ctx context.Context, job *dao.DramaImportJob) error {
	if err := s.packReadyForRequeue(ctx, job); err != nil {
		return err
	}
	if err := s.requeueImportJob(ctx, job.Pk); err != nil {
		return mapRequeueErr(err)
	}
	if err := s.packReadyForRequeue(ctx, job); err != nil {
		return gerror.NewCode(errcode.CodeBadRequest, "任务在转入重试排队后整包已不可用，请重新上传，不要当作已成功排队")
	}
	return nil
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
