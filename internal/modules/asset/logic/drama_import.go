package logic

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/google/uuid"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/dao"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/aesbnc"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

type DramaImportPlan struct {
	TargetID string                `json:"target_id"`
	Items    []DramaImportPlanItem `json:"items"`
}

type DramaImportPlanItem struct {
	Title    string              `json:"title"`
	Cover    string              `json:"cover"`
	TargetID string              `json:"target_id"`
	Episodes []DramaImportPlanEp `json:"episodes"`
}

type DramaImportPlanEp struct {
	Path  string `json:"path"`
	Seq   int    `json:"seq"`
	Title string `json:"title"`
}

func (s *sAsset) importDramasFromPack(ctx context.Context, zipPath, zipName, planJSON string, lease dao.DramaJobLease) (*v1.ImportDramasRes, error) {
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	lim := dramaImportLimits(ctx)
	if err := ctxImportCanceled(ctx); err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}
	st, err := os.Stat(zipPath)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "无法读取压缩包")
	}
	if st.Size() > lim.MaxZipBytes {
		return nil, gerror.NewCode(errcode.CodeBadRequest, fmt.Sprintf("压缩包大小 %s 超过上限 %s", humanBytes(st.Size()), humanBytes(lim.MaxZipBytes)))
	}
	if err := ensureDiskFree(filepath.Dir(zipPath), lim.MinFreeBytes); err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}

	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "无法打开压缩包")
	}
	defer zr.Close()

	metas, err := zipMetasFromReader(&zr.Reader)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}
	if err := checkZipLimits(st.Size(), metas, lim); err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}
	var largest int64
	for _, f := range metas {
		if shouldIgnoreZipName(f.Name) {
			continue
		}
		if f.UncompressedSize > largest {
			largest = f.UncompressedSize
		}
	}
	if err := ensureDiskFree(filepath.Dir(zipPath), lim.MinFreeBytes+largest); err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}

	groups, err := GroupDramaZip(metas, zipName)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}
	if planJSON != "" {
		var raw DramaImportPlan
		if err := json.Unmarshal([]byte(planJSON), &raw); err != nil {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "导入计划格式无效")
		}
		if len(raw.Items) > 0 {
			groups, err = applyDramaPlan(groups, planJSON)
			if err != nil {
				return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
			}
		} else if tid := strings.TrimSpace(raw.TargetID); tid != "" {
			if len(groups) != 1 {
				return nil, gerror.NewCode(errcode.CodeBadRequest, "压缩包含多部短剧时，必须在计划中为各部指定续传目标，不能按标题混剧")
			}
			groups[0].TargetID = tid
		}
	}
	if err := ensureUniqueDramaItemKeys(groups); err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}

	files := map[string]*zip.File{}
	for _, f := range zr.File {
		clean, err := sanitizeZipEntryName(f.Name)
		if err != nil {
			if shouldIgnoreZipName(f.Name) {
				continue
			}
			return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
		}
		files[clean] = f
	}

	res := &v1.ImportDramasRes{
		List:   make([]v1.ImportDramasItem, 0, len(groups)),
		Failed: make([]v1.ImportDramasFail, 0),
	}
	workDir, err := os.MkdirTemp(filepath.Dir(zipPath), "entry-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(workDir)

	for _, g := range groups {
		if err := ctxImportCanceled(ctx); err != nil {
			res.Failed = append(res.Failed, v1.ImportDramasFail{Title: g.Title, Error: err.Error()})
			break
		}
		item, ferr := s.importOneDrama(ctx, g, files, workDir, lim, lease)
		if item != nil {
			res.List = append(res.List, *item)
		}
		if ferr != nil {
			res.Failed = append(res.Failed, v1.ImportDramasFail{Title: g.Title, Error: ferr.Error()})
			if item == nil {
				continue
			}
			// 转码投递失败：原片已入库，仍计为导入成功项
		}
	}
	res.Imported = len(res.List)
	res.FailedCount = len(res.Failed)
	if res.Imported == 0 {
		msg := "没有成功导入的短剧"
		if len(res.Failed) > 0 {
			msg = res.Failed[0].Error
		}
		return nil, gerror.NewCode(errcode.CodeBadRequest, msg)
	}
	return res, nil
}

func ensureUniqueDramaItemKeys(groups []DramaZipGroup) error {
	seen := map[string]string{}
	for _, g := range groups {
		key, err := dao.NormalizeDramaImportItemKey(g.ItemKey)
		if err != nil {
			return err
		}
		if prev, ok := seen[key]; ok {
			return fmt.Errorf("压缩包目录 %q 与 %q 规范化后冲突，拒绝合并导入", prev, g.ItemKey)
		}
		seen[key] = g.ItemKey
	}
	return nil
}

func applyDramaPlan(groups []DramaZipGroup, planJSON string) ([]DramaZipGroup, error) {
	var plan DramaImportPlan
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return nil, fmt.Errorf("导入计划格式无效")
	}
	if len(plan.Items) == 0 {
		return nil, fmt.Errorf("导入计划为空")
	}
	var out []DramaZipGroup
	for _, it := range plan.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			return nil, fmt.Errorf("计划中存在空标题")
		}
		src, ok := findPlanGroup(groups, it)
		if !ok {
			return nil, fmt.Errorf("计划中的短剧「%s」在压缩包中找不到对应文件", title)
		}
		g := src
		g.Title = title
		g.TargetID = strings.TrimSpace(it.TargetID)
		if g.TargetID == "" {
			g.TargetID = strings.TrimSpace(plan.TargetID)
		}
		if it.Cover != "" {
			cover, err := sanitizeZipEntryName(it.Cover)
			if err != nil {
				return nil, fmt.Errorf("短剧「%s」的封面路径无效: %w", title, err)
			}
			g.CoverPath = cover
			if !inGroup(g, g.CoverPath, true) {
				return nil, fmt.Errorf("短剧「%s」的封面不在该目录内", title)
			}
		} else {
			g.CoverPath = ""
		}
		var videos []dramaFile
		seen := map[int]int{}
		for _, ep := range it.Episodes {
			if ep.Seq < 1 {
				return nil, fmt.Errorf("短剧「%s」存在无效集号", title)
			}
			seen[ep.Seq]++
			if seen[ep.Seq] > 1 {
				return nil, fmt.Errorf("短剧「%s」集号 %d 重复", title, ep.Seq)
			}
			if ep.Path == "" {
				return nil, fmt.Errorf("短剧「%s」剧集路径为空", title)
			}
			p, err := sanitizeZipEntryName(ep.Path)
			if err != nil {
				return nil, fmt.Errorf("短剧「%s」剧集路径无效: %w", title, err)
			}
			f, ok := findVideo(g, p)
			if !ok {
				return nil, fmt.Errorf("短剧「%s」找不到剧集文件", title)
			}
			f.Seq, f.Has = ep.Seq, true
			if strings.TrimSpace(ep.Title) != "" {
				f.Name = strings.TrimSpace(ep.Title)
			}
			videos = append(videos, f)
		}
		if len(videos) == 0 {
			return nil, fmt.Errorf("短剧「%s」没有剧集", title)
		}
		g.Videos = videos
		g.Warnings = nil
		g.NeedsConfirm = false
		out = append(out, g)
	}
	return out, nil
}

func findPlanGroup(groups []DramaZipGroup, it DramaImportPlanItem) (DramaZipGroup, bool) {
	if len(it.Episodes) > 0 && it.Episodes[0].Path != "" {
		p, err := sanitizeZipEntryName(it.Episodes[0].Path)
		if err == nil {
			for _, g := range groups {
				if inGroup(g, p, false) {
					return g, true
				}
			}
		}
	}
	for _, g := range groups {
		if g.Title == strings.TrimSpace(it.Title) {
			return g, true
		}
	}
	return DramaZipGroup{}, false
}

func inGroup(g DramaZipGroup, rel string, image bool) bool {
	if image {
		for _, c := range g.Covers {
			if c.Path == rel {
				return true
			}
		}
		return false
	}
	for _, v := range g.Videos {
		if v.Path == rel {
			return true
		}
	}
	return false
}

func findVideo(g DramaZipGroup, rel string) (dramaFile, bool) {
	for _, v := range g.Videos {
		if v.Path == rel {
			return v, true
		}
	}
	return dramaFile{}, false
}

func ctxImportCanceled(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("超时/取消，结果可能部分完成，请刷新核对后再决定重试")
	}
	return nil
}

func (s *sAsset) importOneDrama(ctx context.Context, g DramaZipGroup, files map[string]*zip.File, workDir string, lim DramaImportLimits, lease dao.DramaJobLease) (*v1.ImportDramasItem, error) {
	if err := ctxImportCanceled(ctx); err != nil {
		return nil, err
	}
	if g.NeedsConfirm {
		msg := "需要确认后才能导入"
		if len(g.Warnings) > 0 {
			msg = g.Warnings[0]
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if strings.TrimSpace(g.Title) == "" || len(g.Videos) == 0 {
		return nil, fmt.Errorf("缺少标题或剧集")
	}

	var series *domain.Asset
	created := false
	if err := withDramaImportLease(ctx, lease, func(ctx context.Context) error {
		got, isNew, err := dao.ResolveDramaImportSeries(ctx, lease.JobPK, g.ItemKey, g.Title, g.TargetID)
		if err != nil {
			return err
		}
		series = got
		created = isNew
		return nil
	}); err != nil {
		return nil, err
	}

	skipped := 0
	imported := 0
	var enqueueErrs []string
	for _, v := range g.Videos {
		if err := ctxImportCanceled(ctx); err != nil {
			return nil, err
		}
		epTitle := strings.TrimSpace(v.Name)
		if strings.Contains(epTitle, ".") && videoExt[strings.ToLower(path.Ext(epTitle))] {
			epTitle = "第" + itoa(v.Seq) + "集"
		}
		if epTitle == "" {
			epTitle = "第" + itoa(v.Seq) + "集"
		}
		var code string
		var isNew bool
		var ep *domain.Asset
		if err := withDramaImportLease(ctx, lease, func(ctx context.Context) error {
			var err error
			code, isNew, err = s.repo.CreateEpisode(ctx, series.Pk, v.Seq, epTitle)
			if err != nil {
				return err
			}
			ep, err = s.repo.GetByCode(ctx, code)
			if err != nil || ep == nil {
				return err
			}
			return nil
		}); err != nil {
			return nil, err
		}
		if !isNew && ep.Status == consts.AssetStatusReady {
			skipped++
			continue
		}
		if !isNew && ep.SourceKey != "" && ep.Status == consts.AssetStatusProcessing {
			skipped++
			continue
		}
		zf, ok := files[v.Path]
		if !ok {
			return nil, fmt.Errorf("压缩包缺少文件 %s", v.Path)
		}
		if ep.SourceKey == "" || isNew || ep.Status == consts.AssetStatusDraft || ep.Status == consts.AssetStatusFailed {
			if err := s.putDramaSource(ctx, ep, zf, workDir, lim, lease); err != nil {
				return nil, err
			}
		}
		if _, err := s.triggerTranscodeIfLease(ctx, lease, ep.Code, 8); err != nil {
			enqueueErrs = append(enqueueErrs, fmt.Sprintf("第%d集转码投递失败: %s", v.Seq, err.Error()))
		}
		imported++
	}

	if g.CoverPath != "" {
		if zf, ok := files[g.CoverPath]; ok {
			if err := s.putDramaSeriesCover(ctx, series, zf, lim, lease); err != nil {
				return nil, fmt.Errorf("封面失败: %w", err)
			}
		}
	}
	if err := withDramaImportLease(ctx, lease, func(ctx context.Context) error {
		return s.refreshDramaCount(ctx, series.Pk)
	}); err != nil {
		return nil, err
	}
	count := imported + skipped
	if latest, e := s.repo.GetByCode(ctx, series.Code); e == nil && latest != nil {
		count = latest.ChapterCount
	}
	item := &v1.ImportDramasItem{
		Id: series.Code, Title: g.Title, ChapterCount: count,
		Appended: !created, SkippedCount: skipped, ImportedCount: imported,
	}
	if len(enqueueErrs) > 0 {
		// 原片已入库，任务记为部分成功，由重试补投 Kafka，不得吞掉投递失败。
		return item, fmt.Errorf("%s", strings.Join(enqueueErrs, "；"))
	}
	return item, nil
}

func (s *sAsset) putDramaSource(ctx context.Context, ep *domain.Asset, zf *zip.File, workDir string, lim DramaImportLimits, lease dao.DramaJobLease) error {
	tmp := filepath.Join(workDir, uuid.NewString())
	if err := writeZipFileBounded(zf, tmp, lim.MaxFileBytes); err != nil {
		return err
	}
	defer os.Remove(tmp)
	f, err := os.Open(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	ext := strings.ToLower(path.Ext(zf.Name))
	if !videoExt[ext] {
		ext = ".mp4"
	}
	key := consts.SourceObjectPrefix(consts.KindDrama, ep.Code) + uuid.NewString() + ext
	bucket := s.store.Bucket()
	if err := ctxImportCanceled(ctx); err != nil {
		return err
	}
	if err := s.store.PutObject(ctx, bucket, key, "video/mp4", f, st.Size()); err != nil {
		return gerror.Wrap(err, "上传原片失败")
	}
	err = withDramaImportLease(ctx, lease, func(ctx context.Context) error {
		return s.repo.BindSource(ctx, ep.Pk, bucket, key)
	})
	if err != nil {
		// 绑定未成功且对象 key 为本次 UUID，尽量收回，避免失租后留下无主原片引用。
		if remErr := s.store.RemoveObject(ctx, bucket, key); remErr != nil {
			// 降低残留，不保证删除成功
		}
		return err
	}
	return nil
}

func (s *sAsset) putDramaSeriesCover(ctx context.Context, series *domain.Asset, zf *zip.File, lim DramaImportLimits, lease dao.DramaJobLease) error {
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	plain, err := io.ReadAll(io.LimitReader(rc, maxCoverBytes+1))
	if err != nil {
		return err
	}
	if int64(len(plain)) > maxCoverBytes {
		return fmt.Errorf("封面不能超过 8MB")
	}
	enc, err := aesbnc.Encrypt(plain)
	if err != nil {
		return err
	}
	key := consts.DramaCoverKey(series.Code)
	bucket := s.store.Bucket()
	if err := ctxImportCanceled(ctx); err != nil {
		return err
	}
	if err := s.store.PutObject(ctx, bucket, key, "application/octet-stream", bytes.NewReader(enc), int64(len(enc))); err != nil {
		return err
	}
	// 封面对象键固定，失租时不删除，以免误伤已绑定封面；仅 DB 绑定受租约事务保护。
	return withDramaImportLease(ctx, lease, func(ctx context.Context) error {
		if err := s.repo.UpdateCover(ctx, series.Pk, s.store.PublicURL(bucket, key), key, series.Kind); err != nil {
			return err
		}
		return s.repo.BindSource(ctx, series.Pk, bucket, key)
	})
}

func writeZipFileBounded(zf *zip.File, dest string, max int64) error {
	if zf.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("拒绝符号链接: %s", zf.Name)
	}
	rc, err := zf.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
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
		return fmt.Errorf("文件 %s 超过单文件上限 %s", zf.Name, humanBytes(max))
	}
	return nil
}

func withDramaImportLease(ctx context.Context, lease dao.DramaJobLease, fn func(ctx context.Context) error) error {
	if lease.JobPK <= 0 || strings.TrimSpace(lease.Owner) == "" || lease.Generation <= 0 {
		return fmt.Errorf("缺少有效的导入任务租约")
	}
	return dao.WithDramaJobLease(ctx, lease, fn)
}
