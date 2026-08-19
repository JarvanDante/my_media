package logic

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

func (s *sAsset) ImportComics(ctx context.Context, zipPath string) (*v1.ImportComicsRes, error) {
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
		item, ferr := s.importOneManga(ctx, manga)
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

func (s *sAsset) importOneManga(ctx context.Context, manga parsedManga) (*v1.ImportComicsItem, error) {
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
	coverExt := manga.CoverExt
	if coverFile == nil && len(manga.Chapters) > 0 && len(manga.Chapters[0].Pages) > 0 {
		coverFile = manga.Chapters[0].Pages[0].File
		coverExt = strings.ToLower(path.Ext(manga.Chapters[0].Pages[0].Name))
	}
	if coverExt == "" {
		coverExt = ".jpg"
	}
	coverKey := prefix + "cover" + coverExt
	if coverFile != nil {
		if err := putZipFile(ctx, s.store, bucket, coverKey, guessImageCT(coverExt), coverFile); err != nil {
			rollback()
			return nil, err
		}
	}

	chapters := make([]domain.ComicChapter, 0, len(manga.Chapters))
	pageTotal := 0
	for _, ch := range manga.Chapters {
		pages := make([]domain.ComicPage, 0, len(ch.Pages))
		for i, p := range ch.Pages {
			ext := strings.ToLower(path.Ext(p.Name))
			name := fmt.Sprintf("page_%03d%s", i+1, ext)
			key := fmt.Sprintf("%sch%03d/%s", prefix, ch.Seq, name)
			if err := putZipFile(ctx, s.store, bucket, key, guessImageCT(ext), p.File); err != nil {
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
		out = append(out, v1.ComicChapterItem{
			Seq: ch.Seq, Title: ch.Title, PageCount: ch.PageCount, Pages: pages,
		})
	}
	return out, nil
}

func (s *sAsset) decorateCover(ctx context.Context, a *domain.Asset) {
	if a == nil || a.Kind != consts.KindComics || s.store == nil || a.SourceKey == "" {
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

func putZipFile(ctx context.Context, store interface {
	PutObject(context.Context, string, string, string, io.Reader, int64) error
}, bucket, key, contentType string, f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return store.PutObject(ctx, bucket, key, contentType, rc, int64(f.UncompressedSize64))
}

func guessImageCT(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
