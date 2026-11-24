package logic

import (
	"context"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/frame/g"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

func dramaImportLimits(ctx context.Context) DramaImportLimits {
	lim := DefaultDramaImportLimits()
	if n := g.Cfg().MustGet(ctx, "drama_import.max_zip_bytes", 0).Int64(); n > 0 {
		lim.MaxZipBytes = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.max_extract_bytes", 0).Int64(); n > 0 {
		lim.MaxExtractBytes = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.max_files", 0).Int(); n > 0 {
		lim.MaxFiles = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.max_file_bytes", 0).Int64(); n > 0 {
		lim.MaxFileBytes = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.max_ratio", 0).Float64(); n > 0 {
		lim.MaxRatio = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.min_free_bytes", 0).Int64(); n > 0 {
		lim.MinFreeBytes = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.max_body_bytes", 0).Int64(); n > 0 {
		lim.MaxBodyBytes = n
	}
	if n := g.Cfg().MustGet(ctx, "drama_import.import_timeout_sec", 0).Int(); n > 0 {
		lim.ImportTimeoutSec = n
	}
	return lim.withDefaults()
}

func (s *sAsset) ImportLimits(ctx context.Context) v1.DramaImportLimitsRes {
	lim := dramaImportLimits(ctx)
	return v1.DramaImportLimitsRes{
		MaxZipBytes: lim.MaxZipBytes, MaxBodyBytes: lim.MaxBodyBytes,
		MaxExtractBytes: lim.MaxExtractBytes,
		MaxFiles:        lim.MaxFiles, MaxFileBytes: lim.MaxFileBytes,
		MaxRatio: lim.MaxRatio, MinFreeBytes: lim.MinFreeBytes,
		ImportTimeoutSec: lim.ImportTimeoutSec,
	}
}

func (s *sAsset) CreateDrama(ctx context.Context, title, coverUrl, remark, intro string, updateStatus int) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return "", gerror.NewCode(errcode.CodeBadRequest, "标题必填")
	}
	return s.repo.CreateDrama(ctx, title, coverUrl, remark, strings.TrimSpace(intro), updateStatus)
}

func (s *sAsset) UpdateDrama(ctx context.Context, code, title, remark, intro string, updateStatus int) error {
	a, err := s.requireDramaSeries(ctx, code)
	if err != nil {
		return err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return gerror.NewCode(errcode.CodeBadRequest, "标题必填")
	}
	return s.repo.UpdateDrama(ctx, a.Pk, title, remark, strings.TrimSpace(intro), updateStatus)
}

func (s *sAsset) CreateEpisode(ctx context.Context, parentCode string, seq int, title string) (string, bool, error) {
	parent, err := s.requireDramaSeries(ctx, parentCode)
	if err != nil {
		return "", false, err
	}
	if seq < 1 {
		return "", false, gerror.NewCode(errcode.CodeBadRequest, "集号必须从 1 开始")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "第" + itoa(seq) + "集"
	}
	code, created, err := s.repo.CreateEpisode(ctx, parent.Pk, seq, title)
	if err != nil {
		return "", false, err
	}
	_ = s.refreshDramaCount(ctx, parent.Pk)
	return code, created, nil
}

func (s *sAsset) UpdateEpisode(ctx context.Context, code, title string, seq int) error {
	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return err
	}
	if a == nil || !consts.IsDramaEpisode(a.Kind, a.ParentId) {
		return gerror.NewCode(errcode.CodeBadRequest, "不是短剧剧集")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = a.Title
	}
	if seq < 1 {
		seq = a.Seq
	}
	if seq != a.Seq {
		exist, err := s.repo.FindEpisode(ctx, a.ParentId, seq)
		if err != nil {
			return err
		}
		if exist != nil && exist.Pk != a.Pk {
			return gerror.NewCode(errcode.CodeBadRequest, "集号已存在")
		}
	}
	if err := s.repo.UpdateEpisodeMeta(ctx, a.Pk, title, seq); err != nil {
		return err
	}
	return nil
}

func (s *sAsset) ListEpisodes(ctx context.Context, parentCode string) ([]domain.Asset, error) {
	parent, err := s.requireDramaSeries(ctx, parentCode)
	if err != nil {
		return nil, err
	}
	list, err := s.repo.ListEpisodes(ctx, parent.Pk)
	if err != nil {
		return nil, err
	}
	for i := range list {
		s.decorateCover(ctx, &list[i])
	}
	return list, nil
}

func (s *sAsset) requireDramaSeries(ctx context.Context, code string) (*domain.Asset, error) {
	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	if !consts.IsDramaSeries(a.Kind, a.ParentId) {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "不是短剧整部")
	}
	return a, nil
}

func (s *sAsset) refreshDramaCount(ctx context.Context, parentPK int64) error {
	list, err := s.repo.ListEpisodes(ctx, parentPK)
	if err != nil {
		return err
	}
	return s.repo.UpdateChapterCount(ctx, parentPK, len(list))
}

func toAdminEpisode(a domain.Asset) v1.DramaEpisodeItem {
	return v1.DramaEpisodeItem{
		Id: a.Code, Seq: a.Seq, Title: a.Title, Status: a.Status,
		TranscodeStatus: a.TranscodeStatus, PlayUrl: a.PlayUrl,
		DurationSec: a.DurationSec, SourceKey: a.SourceKey,
		CoverUrl: a.CoverUrl, TranscodeError: a.TranscodeError,
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
