package logic

import (
	"archive/zip"
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/gogf/gf/v2/errors/gerror"

	v1 "github.com/JarvanDante/my_media/api/admin/asset/v1"
	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/aesbnc"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

func (s *sAsset) ImportNovels(ctx context.Context, zipPath string, resume bool) (*v1.ImportNovelsRes, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "无法打开压缩包")
	}
	defer zr.Close()

	books, err := parseNovelsZip(&zr.Reader)
	if err != nil {
		return nil, gerror.NewCode(errcode.CodeBadRequest, err.Error())
	}

	res := &v1.ImportNovelsRes{
		List:   make([]v1.ImportNovelsItem, 0, len(books)),
		Failed: make([]v1.ImportNovelsFail, 0),
	}
	for _, book := range books {
		item, ferr := s.importOneNovel(ctx, book, resume)
		if ferr != nil {
			res.Failed = append(res.Failed, v1.ImportNovelsFail{Title: book.Title, Error: ferr.Error()})
			continue
		}
		res.List = append(res.List, *item)
	}
	res.Imported = len(res.List)
	res.FailedCount = len(res.Failed)
	if res.Imported == 0 {
		msg := "没有成功导入的小说"
		if len(res.Failed) > 0 {
			msg = res.Failed[0].Error
		}
		return nil, gerror.NewCode(errcode.CodeBadRequest, msg)
	}
	return res, nil
}

func (s *sAsset) importOneNovel(ctx context.Context, book parsedNovel, resume bool) (*v1.ImportNovelsItem, error) {
	exist, err := s.repo.FindNovelByTitle(ctx, book.Title)
	if err != nil {
		return nil, err
	}
	if exist != nil {
		if !resume {
			return nil, gerror.NewCode(errcode.CodeBadRequest, "小说名称已经存在")
		}
		return s.appendOneNovel(ctx, exist, book)
	}
	return s.createOneNovel(ctx, book)
}

func skipExistingNovelSeqs(chapters []parsedNovelChapter, exist []domain.NovelChapter) (keep []parsedNovelChapter, skipped int) {
	have := make(map[int]struct{}, len(exist))
	for _, ch := range exist {
		have[ch.Seq] = struct{}{}
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

func toNovelChapters(in []parsedNovelChapter) []domain.NovelChapter {
	out := make([]domain.NovelChapter, 0, len(in))
	for _, ch := range in {
		out = append(out, domain.NovelChapter{
			Seq: ch.Seq, Title: ch.Title, Content: ch.Content, WordCount: utf8.RuneCountInString(ch.Content),
		})
	}
	return out
}

func sumNovelWords(chs []domain.NovelChapter) int {
	n := 0
	for _, ch := range chs {
		n += ch.WordCount
	}
	return n
}

func (s *sAsset) appendOneNovel(ctx context.Context, exist *domain.Asset, book parsedNovel) (*v1.ImportNovelsItem, error) {
	existChs, err := s.repo.ListNovelChapters(ctx, exist.Pk)
	if err != nil {
		return nil, err
	}
	incoming, skipped := skipExistingNovelSeqs(book.Chapters, existChs)
	if len(incoming) == 0 {
		return &v1.ImportNovelsItem{
			Id: exist.Code, Title: exist.Title, Category: exist.Category,
			ChapterCount: 0, WordCount: 0, Appended: true, SkippedCount: skipped,
		}, nil
	}
	chapters := toNovelChapters(incoming)
	if err := s.repo.AppendNovelChapters(ctx, exist.Pk, chapters); err != nil {
		return nil, err
	}
	remain := append(append([]domain.NovelChapter{}, existChs...), chapters...)
	if err := s.repo.UpdateNovelStats(ctx, exist.Pk, len(remain), sumNovelWords(remain)); err != nil {
		return nil, err
	}
	return &v1.ImportNovelsItem{
		Id: exist.Code, Title: exist.Title, Category: exist.Category,
		ChapterCount: len(chapters), WordCount: sumNovelWords(chapters),
		Appended: true, SkippedCount: skipped,
	}, nil
}

func (s *sAsset) createOneNovel(ctx context.Context, book parsedNovel) (*v1.ImportNovelsItem, error) {
	pk, code, err := s.repo.CreateNovel(ctx, domain.NovelCreateInput{
		Title: book.Title, Category: book.Category, Intro: book.Intro,
	})
	if err != nil {
		return nil, err
	}
	rollback := func() {
		_ = s.repo.Delete(ctx, pk)
	}

	var bucket, coverKey, coverURL string
	if book.Cover != nil {
		if s.store == nil {
			rollback()
			return nil, gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化，无法写入封面")
		}
		bucket = s.store.Bucket()
		coverKey = consts.PrefixNovel + code + "/cover" + aesbnc.Ext
		if err := putZipEncrypted(ctx, s.store, bucket, coverKey, book.Cover); err != nil {
			rollback()
			return nil, err
		}
		coverURL = s.store.PublicURL(bucket, coverKey)
	}

	chapters := toNovelChapters(book.Chapters)
	if err := s.repo.ReplaceNovelChapters(ctx, pk, chapters); err != nil {
		if bucket != "" && coverKey != "" {
			_, _ = s.store.RemovePrefix(ctx, bucket, consts.PrefixNovel+code+"/")
		}
		rollback()
		return nil, err
	}
	if err := s.repo.UpdateNovelReady(ctx, pk, bucket, coverKey, coverURL, len(chapters), sumNovelWords(chapters)); err != nil {
		if bucket != "" && coverKey != "" {
			_, _ = s.store.RemovePrefix(ctx, bucket, consts.PrefixNovel+code+"/")
		}
		rollback()
		return nil, err
	}
	return &v1.ImportNovelsItem{
		Id: code, Title: book.Title, Category: book.Category,
		ChapterCount: len(chapters), WordCount: sumNovelWords(chapters),
	}, nil
}

func (s *sAsset) NovelChapters(ctx context.Context, code string) ([]v1.ComicChapterItem, error) {
	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	if a.Kind != consts.KindNovel {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "该资产不是小说")
	}
	chs, err := s.repo.ListNovelChapters(ctx, a.Pk)
	if err != nil {
		return nil, err
	}
	out := make([]v1.ComicChapterItem, 0, len(chs))
	for _, ch := range chs {
		out = append(out, v1.ComicChapterItem{
			Seq: ch.Seq, Title: ch.Title, Content: ch.Content, WordCount: ch.WordCount,
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

func findNovelChapter(chs []domain.NovelChapter, seq int) *domain.NovelChapter {
	if seq <= 0 {
		return nil
	}
	for i := range chs {
		if chs[i].Seq == seq {
			return &chs[i]
		}
	}
	return nil
}

func (s *sAsset) deleteNovelChapter(ctx context.Context, a *domain.Asset, seq int) (int, int, error) {
	chs, err := s.repo.ListNovelChapters(ctx, a.Pk)
	if err != nil {
		return 0, 0, err
	}
	ch := findNovelChapter(chs, seq)
	if ch == nil {
		return 0, 0, gerror.NewCode(errcode.CodeNotFound, "章节不存在")
	}
	if err := s.repo.DeleteNovelChapter(ctx, a.Pk, ch.Seq); err != nil {
		return 0, len(chs), gerror.Wrap(err, "删除章节记录失败")
	}
	remain := make([]domain.NovelChapter, 0, len(chs)-1)
	for _, x := range chs {
		if x.Seq != ch.Seq {
			remain = append(remain, x)
		}
	}
	if err := s.repo.UpdateNovelStats(ctx, a.Pk, len(remain), sumNovelWords(remain)); err != nil {
		return 0, len(remain), gerror.Wrap(err, "章节已删但更新章数失败")
	}
	return 0, len(remain), nil
}

func (s *sAsset) novelAsset(ctx context.Context, code string) (*domain.Asset, error) {
	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}
	if a.Kind != consts.KindNovel {
		return nil, gerror.NewCode(errcode.CodeBadRequest, "该资产不是小说")
	}
	return a, nil
}

func nextNovelSeq(chs []domain.NovelChapter) int {
	next := 1
	for _, ch := range chs {
		if ch.Seq >= next {
			next = ch.Seq + 1
		}
	}
	return next
}

func (s *sAsset) CreateNovelChapter(ctx context.Context, code string, seq int, title, content string) (int, int, int, error) {
	a, err := s.novelAsset(ctx, code)
	if err != nil {
		return 0, 0, 0, err
	}
	chs, err := s.repo.ListNovelChapters(ctx, a.Pk)
	if err != nil {
		return 0, 0, 0, err
	}
	if seq <= 0 {
		seq = nextNovelSeq(chs)
	}
	if findNovelChapter(chs, seq) != nil {
		return 0, 0, 0, gerror.NewCode(errcode.CodeBadRequest, "章序已存在")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = fmt.Sprintf("第%d章", seq)
	}
	wc := utf8.RuneCountInString(content)
	ch := domain.NovelChapter{Seq: seq, Title: title, Content: content, WordCount: wc}
	if err := s.repo.AppendNovelChapters(ctx, a.Pk, []domain.NovelChapter{ch}); err != nil {
		return 0, 0, 0, gerror.Wrap(err, "新增章节失败")
	}
	remain := append(append([]domain.NovelChapter{}, chs...), ch)
	if err := s.repo.UpdateNovelStats(ctx, a.Pk, len(remain), sumNovelWords(remain)); err != nil {
		return seq, wc, len(remain), gerror.Wrap(err, "章节已写入但更新统计失败")
	}
	return seq, wc, len(remain), nil
}

func (s *sAsset) UpdateNovelChapter(ctx context.Context, code string, seq int, title, content string) (int, int, error) {
	a, err := s.novelAsset(ctx, code)
	if err != nil {
		return 0, 0, err
	}
	chs, err := s.repo.ListNovelChapters(ctx, a.Pk)
	if err != nil {
		return 0, 0, err
	}
	exist := findNovelChapter(chs, seq)
	if exist == nil {
		return 0, 0, gerror.NewCode(errcode.CodeNotFound, "章节不存在")
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = exist.Title
	}
	wc := utf8.RuneCountInString(content)
	if err := s.repo.UpdateNovelChapter(ctx, a.Pk, domain.NovelChapter{
		Seq: seq, Title: title, Content: content, WordCount: wc,
	}); err != nil {
		return 0, 0, gerror.Wrap(err, "保存章节失败")
	}
	for i := range chs {
		if chs[i].Seq == seq {
			chs[i].Title = title
			chs[i].Content = content
			chs[i].WordCount = wc
			break
		}
	}
	if err := s.repo.UpdateNovelStats(ctx, a.Pk, len(chs), sumNovelWords(chs)); err != nil {
		return wc, len(chs), gerror.Wrap(err, "章节已保存但更新统计失败")
	}
	return wc, len(chs), nil
}

