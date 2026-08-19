package dao

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
	"github.com/JarvanDante/my_media/internal/shared/kit"
)

func (r *assetRepo) CreateComics(ctx context.Context, in domain.ComicsCreateInput) (int64, string, error) {
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
			"kind":             consts.KindComics,
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

func (r *assetRepo) UpdateComicsReady(ctx context.Context, pk int64, bucket, coverKey, coverURL string, chapterCount int) error {
	_, err := g.DB().Model("media_asset").Ctx(ctx).Where("id", pk).Data(g.Map{
		"source_bucket": bucket,
		"source_key":    coverKey,
		"cover_url":     coverURL,
		"chapter_count": chapterCount,
		"status":        consts.AssetStatusReady,
		"updated_at":    gtime.Now(),
	}).Update()
	return err
}

func (r *assetRepo) ReplaceComicChapters(ctx context.Context, assetID int64, chapters []domain.ComicChapter) error {
	return g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		if _, err := tx.Model("media_comic_chapter").Ctx(ctx).Where("asset_id", assetID).Delete(); err != nil {
			return err
		}
		for _, ch := range chapters {
			pages, _ := json.Marshal(ch.Pages)
			if pages == nil {
				pages = []byte("[]")
			}
			if _, err := tx.Model("media_comic_chapter").Ctx(ctx).Data(g.Map{
				"asset_id":   assetID,
				"seq":        ch.Seq,
				"title":      ch.Title,
				"page_count": ch.PageCount,
				"pages":      string(pages),
				"created_at": gtime.Now(),
			}).Insert(); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *assetRepo) ListComicChapters(ctx context.Context, assetID int64) ([]domain.ComicChapter, error) {
	rows, err := g.DB().Model("media_comic_chapter").Ctx(ctx).
		Where("asset_id", assetID).OrderAsc("seq").All()
	if err != nil {
		return nil, err
	}
	out := make([]domain.ComicChapter, 0, len(rows))
	for _, row := range rows {
		var pages []domain.ComicPage
		_ = json.Unmarshal([]byte(row["pages"].String()), &pages)
		out = append(out, domain.ComicChapter{
			Seq:       row["seq"].Int(),
			Title:     row["title"].String(),
			PageCount: row["page_count"].Int(),
			Pages:     pages,
		})
	}
	return out, nil
}
