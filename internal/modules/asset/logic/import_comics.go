package logic

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/aesbnc"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

func (s *sAsset) ImportComics(ctx context.Context, zipPath string, resume bool) (*v1.ImportComicsRes, error) {
	if s.store == nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "无法打开压缩包")
	}
	defer zr.Close()

	mangas, err := parseComicsZip(&zr.Reader)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}

	res := &v1.ImportComicsRes{
		List:   make([]v1.ImportComicsItem, 0, len(mangas)),
		Failed: make([]v1.ImportComicsFail, 0),
	}
	for _, manga := range mangas {
		item, ferr := s.importOneManga(ctx, manga, resume)
		if ferr != nil {
			res.Failed = append(res.Failed, v1.ImportComicsFail{Title: manga.Title, Error: ferr.Error()})
			continue
		}
		res.List = append(res.List, *item)
	}
	res.Imported = len(res.List)
	res.FailedCount = len(res.Failed)
	if res.Imported == 0 {
		msg := "没有成功导入的漫画"
		if len(res.Failed) > 0 {
			msg = res.Failed[0].Error
		}
		return nil, gerror.NewCode(errcode.CodeBadRequest, msg)
	}
	return res, nil
}

func (s *sAsset) importOneManga(ctx context.Context, manga parsedManga, resume bool) (*v1.ImportComicsItem, error) {
	exist, err := s.repo.FindComicsByTitle(ctx, manga.Title)
	if err != nil {
		return nil, err
	}
	if exist != nil {
		if !resume {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "漫画名称已经存在")
		}
		return s.appendOneManga(ctx, exist, manga)
	}
	return s.createOneManga(ctx, manga)
}

func skipExistingChapterSeqs(chapters []parsedChapter, exist []domain.ComicChapter) (keep []parsedChapter, skipped int) {
	have := make(map[int]struct{}, len(exist))
	for _, ch := range exist {
		have[effectiveChapterSeq(ch.Seq, ch.Title)] = struct{}{}
	}
	for _, ch := range chapters {
		if _, ok := have[ch.Seq]; ok {
			skipped++
			continue
		}
		keep = append(keep, ch)
	}
	return keep, skipped
}

func (s *sAsset) appendOneManga(ctx context.Context, exist *domain.Asset, manga parsedManga) (*v1.ImportComicsItem, error) {
	existChs, err := s.repo.ListComicChapters(ctx, exist.Pk)
	if err != nil {
		return nil, err
	}
	incoming, skipped := skipExistingChapterSeqs(manga.Chapters, existChs)
	if len(incoming) == 0 {
		return &v1.ImportComicsItem{
			Id: exist.Code, Title: exist.Title, Category: exist.Category,
			ChapterCount: 0, PageCount: 0, Appended: true, SkippedCount: skipped,
		}, nil
	}

	bucket := exist.SourceBucket
	if bucket == "" {
		bucket = s.store.Bucket()
	}
	prefix := consts.PrefixComics + exist.Code + "/"
	var uploaded []string
	rollbackNew := func() {
		for _, key := range uploaded {
			_ = s.store.RemoveObject(ctx, bucket, key)
		}
	}

	chapters := make([]domain.ComicChapter, 0, len(incoming))
	pageTotal := 0
	for _, ch := range incoming {
		pages := make([]domain.ComicPage, 0, len(ch.Pages))
		for i, p := range ch.Pages {
			name := fmt.Sprintf("page_%03d%s", i+1, aesbnc.Ext)
			key := fmt.Sprintf("%sch%03d/%s", prefix, ch.Seq, name)
			if err := putZipEncrypted(ctx, s.store, bucket, key, p.File); err != nil {
				rollbackNew()
				return nil, err
			}
			uploaded = append(uploaded, key)
			pages = append(pages, domain.ComicPage{Key: key, Filename: name})
		}
		pageTotal += len(pages)
		chapters = append(chapters, domain.ComicChapter{
			Seq: ch.Seq, Title: ch.Title, PageCount: len(pages), Pages: pages,
		})
	}
	if err := s.repo.AppendComicChapters(ctx, exist.Pk, chapters); err != nil {
		rollbackNew()
		return nil, err
	}
	if err := s.repo.UpdateComicsChapterCount(ctx, exist.Pk, len(existChs)+len(chapters)); err != nil {
		return nil, err
	}
	return &v1.ImportComicsItem{
		Id: exist.Code, Title: exist.Title, Category: exist.Category,
		ChapterCount: len(chapters), PageCount: pageTotal,
		Appended: true, SkippedCount: skipped,
	}, nil
}

func (s *sAsset) createOneManga(ctx context.Context, manga parsedManga) (*v1.ImportComicsItem, error) {
	remark := manga.Author
	if remark != "" {
		remark = "作者: " + remark
	}
	pk, code, err := s.repo.CreateComics(ctx, domain.ComicsCreateInput{
		Title: manga.Title, Category: manga.Category, Intro: manga.Intro, Remark: remark,
	})
	if err != nil {
		return nil, err
	}
	bucket := s.store.Bucket()
	prefix := consts.PrefixComics + code + "/"
	rollback := func() {
		_, _ = s.store.RemovePrefix(ctx, bucket, prefix)
		_ = s.repo.Delete(ctx, pk)
	}

	coverFile := manga.Cover
	if coverFile == nil && len(manga.Chapters) > 0 && len(manga.Chapters[0].Pages) > 0 {
		coverFile = manga.Chapters[0].Pages[0].File
	}
	coverKey := prefix + "cover" + aesbnc.Ext
	if coverFile != nil {
		if err := putZipEncrypted(ctx, s.store, bucket, coverKey, coverFile); err != nil {
			rollback()
			return nil, err
		}
	}

	chapters := make([]domain.ComicChapter, 0, len(manga.Chapters))
	pageTotal := 0
	for _, ch := range manga.Chapters {
		pages := make([]domain.ComicPage, 0, len(ch.Pages))
		for i, p := range ch.Pages {
			// 内页与封面一致：AES 后统一 page_XXX.bnc（zip 源仍可为 jpg/png）
			name := fmt.Sprintf("page_%03d%s", i+1, aesbnc.Ext)
			key := fmt.Sprintf("%sch%03d/%s", prefix, ch.Seq, name)
			if err := putZipEncrypted(ctx, s.store, bucket, key, p.File); err != nil {
				rollback()
				return nil, err
			}
			pages = append(pages, domain.ComicPage{Key: key, Filename: name})
		}
		pageTotal += len(pages)
		chapters = append(chapters, domain.ComicChapter{
			Seq: ch.Seq, Title: ch.Title, PageCount: len(pages), Pages: pages,
		})
	}
	if err := s.repo.ReplaceComicChapters(ctx, pk, chapters); err != nil {
		rollback()
		return nil, err
	}
	coverURL := s.store.PublicURL(bucket, coverKey)
	if err := s.repo.UpdateComicsReady(ctx, pk, bucket, coverKey, coverURL, len(chapters)); err != nil {
		rollback()
		return nil, err
	}
	return &v1.ImportComicsItem{
		Id: code, Title: manga.Title, Category: manga.Category,
		ChapterCount: len(chapters), PageCount: pageTotal,
	}, nil
}

func (s *sAsset) ComicChapters(ctx context.Context, code string) ([]v1.ComicChapterItem, error) {
	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	if a.Kind != consts.KindComics {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "该资产不是漫画")
	}
	chs, err := s.repo.ListComicChapters(ctx, a.Pk)
	if err != nil {
		return nil, err
	}
	out := make([]v1.ComicChapterItem, 0, len(chs))
	for _, ch := range chs {
		pages := make([]v1.ComicPageItem, 0, len(ch.Pages))
		for _, p := range ch.Pages {
			pages = append(pages, v1.ComicPageItem{
				Filename: p.Filename, Key: p.Key,
				Url: s.signObject(ctx, a.SourceBucket, p.Key),
			})
		}
		sort.SliceStable(pages, func(i, j int) bool {
			si, sj := pageSeq(pages[i].Filename), pageSeq(pages[j].Filename)
			if si != sj {
				return si < sj
			}
			return naturalLess(pages[i].Filename, pages[j].Filename)
		})
		out = append(out, v1.ComicChapterItem{
			Seq: effectiveChapterSeq(ch.Seq, ch.Title), Title: ch.Title, PageCount: ch.PageCount, Pages: pages,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		return naturalLess(out[i].Title, out[j].Title)
	})
	return out, nil
}

func findComicChapter(chs []domain.ComicChapter, seq int) *domain.ComicChapter {
	if seq <= 0 {
		return nil
	}
	for i := range chs {
		if chs[i].Seq == seq {
			return &chs[i]
		}
	}
	for i := range chs {
		if effectiveChapterSeq(chs[i].Seq, chs[i].Title) == seq {
			return &chs[i]
		}
	}
	return nil
}

func (s *sAsset) DeleteChapter(ctx context.Context, code string, seq int) (int, int, error) {
	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return 0, 0, err
	}
	if a == nil {
		return 0, 0, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	if a.Kind == consts.KindNovel {
		return s.deleteNovelChapter(ctx, a, seq)
	}
	if a.Kind != consts.KindComics {
		return 0, 0, gerror.NewCode(errcode.CodeBadRequest, "该资产不是漫画或小说")
	}
	if s.store == nil {
		return 0, 0, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化，拒绝删除以免残留对象")
	}
	chs, err := s.repo.ListComicChapters(ctx, a.Pk)
	if err != nil {
		return 0, 0, err
	}
	ch := findComicChapter(chs, seq)
	if ch == nil {
		return 0, 0, gerror.NewCode(errcode.CodeNotFound, "章节不存在")
	}

	bucket := a.SourceBucket
	if bucket == "" {
		bucket = s.store.Bucket()
	}
	deleted := 0
	seen := map[string]struct{}{}
	removeKey := func(key string) error {
		key = strings.TrimLeft(key, "/")
		if key == "" {
			return nil
		}
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}
		if err := s.store.RemoveObject(ctx, bucket, key); err != nil {
			return err
		}
		deleted++
		return nil
	}
	for _, p := range ch.Pages {
		if err := removeKey(p.Key); err != nil {
			return deleted, len(chs), gerror.Wrapf(err, "清理页图失败(%s)", p.Key)
		}
	}
	prefix := fmt.Sprintf("%s%s/ch%03d/", consts.PrefixComics, a.Code, ch.Seq)
	n, remErr := s.store.RemovePrefix(ctx, bucket, prefix)
	if remErr != nil {
		return deleted, len(chs), gerror.Wrapf(remErr, "清理章节目录失败(%s)", prefix)
	}
	deleted += n

	if err := s.repo.DeleteComicChapter(ctx, a.Pk, ch.Seq); err != nil {
		return deleted, len(chs), gerror.Wrap(err, "对象已清理但删除章节记录失败，请重试")
	}
	remain := len(chs) - 1
	if remain < 0 {
		remain = 0
	}
	if err := s.repo.UpdateComicsChapterCount(ctx, a.Pk, remain); err != nil {
		return deleted, remain, gerror.Wrap(err, "章节已删但更新章数失败")
	}
	return deleted, remain, nil
}

func (s *sAsset) decorateCover(ctx context.Context, a *domain.Asset) {
	if a == nil || s.store == nil || a.SourceKey == "" {
		return
	}
	if a.Kind != consts.KindComics && a.Kind != consts.KindNovel && !consts.IsDramaSeries(a.Kind, a.ParentId) {
		return
	}
	if u := s.signObject(ctx, a.SourceBucket, a.SourceKey); u != "" {
		a.CoverUrl = u
	}
}

func (s *sAsset) signObject(ctx context.Context, bucket, key string) string {
	if s.store == nil || key == "" {
		return ""
	}
	u, err := s.store.PresignGet(ctx, bucket, key)
	if err != nil {
		return ""
	}
	return u
}

func putZipEncrypted(ctx context.Context, store interface {
	PutObject(context.Context, string, string, string, io.Reader, int64) error
}, bucket, key string, f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	plain, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return err
	}
	enc, err := aesbnc.Encrypt(plain)
	if err != nil {
		return err
	}
	return store.PutObject(ctx, bucket, key, "application/octet-stream", bytes.NewReader(enc), int64(len(enc)))
}
