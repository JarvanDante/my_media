package dao

import (
	"context"
	"errors"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/kit"
)

func (r *assetRepo) CreateNovel(ctx context.Context, in domain.NovelCreateInput) (int64, string, error) {
	for i := 0; i < 8; i++ {
		c, err := kit.NewPublicID()
		if err != nil {
			return 0, "", err
		}
		exist, err := g.DB().Model("media_asset").Ctx(ctx).Where("code", c).One()
		if err != nil {
			return 0, "", err
		}
		if !exist.IsEmpty() {
			continue
		}
		id, err := g.DB().Model("media_asset").Ctx(ctx).Data(g.Map{
			"code":             c,
			"title":            in.Title,
			"cover_url":        in.CoverUrl,
			"category":         in.Category,
			"intro":            in.Intro,
			"remark":           in.Remark,
			"kind":             consts.KindNovel,
			"status":           consts.AssetStatusProcessing,
			"transcode_status": "none",
			"created_at":       gtime.Now(),
			"updated_at":       gtime.Now(),
		}).InsertAndGetId()
		if err != nil {
			return 0, "", err
		}
		return id, c, nil
	}
	return 0, "", errors.New("生成资产短码失败, 请重试")
}

func (r *assetRepo) FindNovelByTitle(ctx context.Context, title string) (*domain.Asset, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, nil
	}
	row, err := g.DB().Model("media_asset").Ctx(ctx).
		Where("kind", consts.KindNovel).
		Where("title", title).
		OrderAsc("id").
		One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	a := mapAsset(row.Map())
	return &a, nil
}

func (r *assetRepo) UpdateNovelReady(ctx context.Context, pk int64, bucket, coverKey, coverURL string, chapterCount, wordCount int) error {
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", pk).Data(g.Map{
		"source_bucket": bucket,
		"source_key":    coverKey,
		"cover_url":     coverURL,
		"chapter_count": chapterCount,
		"word_count":    wordCount,
		"status":        consts.AssetStatusReady,
		"updated_at":    gtime.Now(),
	}).Update()
	return err
}

func (r *assetRepo) UpdateNovelStats(ctx context.Context, pk int64, chapterCount, wordCount int) error {
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", pk).Data(g.Map{
		"chapter_count": chapterCount,
		"word_count":    wordCount,
		"status":        consts.AssetStatusReady,
		"updated_at":    gtime.Now(),
	}).Update()
	return err
}

func (r *assetRepo) ReplaceNovelChapters(ctx context.Context, assetID int64, chapters []domain.NovelChapter) error {
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		if _, err := tx.Model("media_novel_chapter").Ctx(ctx).Where("asset_id", assetID).Delete(); err != nil {
			return err
		}
		return insertNovelChapters(ctx, tx, assetID, chapters)
	})
}

func (r *assetRepo) AppendNovelChapters(ctx context.Context, assetID int64, chapters []domain.NovelChapter) error {
	if len(chapters) == 0 {
		return nil
	}
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		return insertNovelChapters(ctx, tx, assetID, chapters)
	})
}

func insertNovelChapters(ctx context.Context, tx gdb.TX, assetID int64, chapters []domain.NovelChapter) error {
	for _, ch := range chapters {
		if _, err := tx.Model("media_novel_chapter").Ctx(ctx).Data(g.Map{
			"asset_id":   assetID,
			"seq":        ch.Seq,
			"title":      ch.Title,
			"content":    ch.Content,
			"word_count": ch.WordCount,
			"created_at": gtime.Now(),
		}).Insert(); err != nil {
			return err
		}
	}
	return nil
}

func (r *assetRepo) DeleteNovelChapter(ctx context.Context, assetID int64, seq int) error {
	if assetID <= 0 || seq <= 0 {
		return errors.New("invalid chapter")
	}
	_, err := g.DB().Model("media_novel_chapter").Ctx(ctx).
		Where("asset_id", assetID).Where("seq", seq).Delete()
	return err
}

func (r *assetRepo) ListNovelChapters(ctx context.Context, assetID int64) ([]domain.NovelChapter, error) {
	rows, err := g.DB().Model("media_novel_chapter").Ctx(ctx).
		Where("asset_id", assetID).OrderAsc("seq").All()
	if err != nil {
		return nil, err
	}
	out := make([]domain.NovelChapter, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.NovelChapter{
			Seq:       row["seq"].Int(),
			Title:     row["title"].String(),
			Content:   row["content"].String(),
			WordCount: row["word_count"].Int(),
		})
	}
	return out, nil
}
