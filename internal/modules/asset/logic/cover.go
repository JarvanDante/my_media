package logic

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"

	"github.com/gogf/gf/v2/errors/gerror"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/shared/aesbnc"
	"github.com/JarvanDante/my_media/internal/shared/errcode"
)

const maxCoverBytes = 8 << 20

func (s *sAsset) ReplaceCover(ctx context.Context, code, filename string, body io.Reader, size int64) error {
	if s.store == nil {
		return gerror.NewCode(errcode.CodeBadRequest, "MinIO 未初始化")
	}
	if body == nil || size <= 0 {
		return gerror.NewCode(errcode.CodeBadRequest, "请选择封面图片")
	}
	if size > maxCoverBytes {
		return gerror.NewCode(errcode.CodeBadRequest, "封面不能超过 8MB")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp":
	default:
		return gerror.NewCode(errcode.CodeBadRequest, "封面只支持 jpg/png/webp")
	}

	a, err := s.repo.GetByCode(ctx, code)
	if err != nil {
		return err
	}
	if a == nil {
		return gerror.NewCode(errcode.CodeNotFound, "资产不存在")
	}

	plain, err := io.ReadAll(io.LimitReader(body, maxCoverBytes+1))
	if err != nil {
		return gerror.Wrap(err, "读取封面失败")
	}
	if int64(len(plain)) > maxCoverBytes {
		return gerror.NewCode(errcode.CodeBadRequest, "封面不能超过 8MB")
	}
	enc, err := aesbnc.Encrypt(plain)
	if err != nil {
		return gerror.Wrap(err, "封面加密失败")
	}

	key := consts.HLSObjectPrefix(a.Kind, a.Code) + "cover.bnc"
	if a.Kind == consts.KindComics {
		key = consts.PrefixComics + a.Code + "/cover.bnc"
	}
	bucket := a.SourceBucket
	if bucket == "" {
		bucket = s.store.Bucket()
	}
	if err := s.store.PutObject(ctx, bucket, key, "application/octet-stream", bytes.NewReader(enc), int64(len(enc))); err != nil {
		return gerror.Wrap(err, "上传封面失败")
	}
	oldJpg := strings.TrimSuffix(key, ".bnc") + ".jpg"
	if oldJpg != key {
		_ = s.store.RemoveObject(ctx, bucket, oldJpg)
	}
	if a.Kind == consts.KindComics && a.SourceKey != "" && a.SourceKey != key {
		_ = s.store.RemoveObject(ctx, bucket, a.SourceKey)
	}
	return s.repo.UpdateCover(ctx, a.Pk, s.store.PublicURL(bucket, key), key, a.Kind)
}
