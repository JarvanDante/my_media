package dao

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/modules/asset/domain"
)

// DramaImportItemBinding 任务项（规范目录）到整部短剧的持久绑定。
type DramaImportItemBinding struct {
	JobPK     int64
	ItemKey   string
	DramaCode string
	Explicit  bool
}

func NormalizeDramaImportItemKey(key string) (string, error) {
	key = strings.ReplaceAll(strings.TrimSpace(key), "\\", "/")
	if key == "" {
		return "", errors.New("缺少稳定的导入项标识")
	}
	clean := path.Clean(key)
	if clean == "" || clean == "/" || strings.HasPrefix(clean, "../") || path.IsAbs(clean) {
		return "", errors.New("导入项标识无效")
	}
	if len(clean) > 512 {
		return "", errors.New("导入项标识过长")
	}
	return clean, nil
}

func GetDramaImportItemBinding(ctx context.Context, jobPK int64, itemKey string) (*DramaImportItemBinding, error) {
	return getDramaImportItemBindingDB(g.DB(), ctx, jobPK, itemKey)
}

func getDramaImportItemBindingDB(db gdb.DB, ctx context.Context, jobPK int64, itemKey string) (*DramaImportItemBinding, error) {
	key, err := NormalizeDramaImportItemKey(itemKey)
	if err != nil {
		return nil, err
	}
	if jobPK <= 0 {
		return nil, errors.New("缺少导入任务")
	}
	row, err := db.Model("drama_import_job_item").Ctx(ctx).
		Where("job_id", jobPK).Where("item_key", key).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	m := row.Map()
	return &DramaImportItemBinding{
		JobPK:     jobPK,
		ItemKey:   toStr(m["item_key"]),
		DramaCode: toStr(m["drama_code"]),
		Explicit:  toBool(m["explicit"]),
	}, nil
}

func insertDramaImportItemBindingDB(db gdb.DB, ctx context.Context, jobPK int64, itemKey, dramaCode string, explicit bool) error {
	key, err := NormalizeDramaImportItemKey(itemKey)
	if err != nil {
		return err
	}
	dramaCode = strings.TrimSpace(dramaCode)
	if jobPK <= 0 || dramaCode == "" {
		return errors.New("缺少导入任务或整部短码")
	}
	now := gtime.Now()
	_, err = db.Model("drama_import_job_item").Ctx(ctx).Data(g.Map{
		"job_id":     jobPK,
		"item_key":   key,
		"drama_code": dramaCode,
		"explicit":   explicit,
		"created_at": now,
		"updated_at": now,
	}).Insert()
	return err
}

func toBool(v interface{}) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int, int32, int64:
		return fmt.Sprintf("%v", x) != "0"
	case []byte:
		s := strings.ToLower(string(x))
		return s == "t" || s == "true" || s == "1"
	case string:
		s := strings.ToLower(x)
		return s == "t" || s == "true" || s == "1"
	default:
		s := strings.ToLower(fmt.Sprintf("%v", v))
		return s == "t" || s == "true" || s == "1"
	}
}

// ResolveDramaImportSeries 须在有效租约的同一短事务中调用：按规范目录读写绑定并创建或校验整部。
// 不按标题匹配；绑定目标缺失或显式 target 冲突时明确失败，不静默再造。
func ResolveDramaImportSeries(ctx context.Context, jobPK int64, itemKey, title, explicitTarget string) (*domain.Asset, bool, error) {
	return resolveDramaImportSeriesDB(g.DB(), ctx, jobPK, itemKey, title, explicitTarget)
}

func resolveDramaImportSeriesDB(db gdb.DB, ctx context.Context, jobPK int64, itemKey, title, explicitTarget string) (*domain.Asset, bool, error) {
	key, err := NormalizeDramaImportItemKey(itemKey)
	if err != nil {
		return nil, false, err
	}
	if jobPK <= 0 {
		return nil, false, errors.New("缺少导入任务")
	}
	explicitTarget = strings.TrimSpace(explicitTarget)
	bind, err := getDramaImportItemBindingDB(db, ctx, jobPK, key)
	if err != nil {
		return nil, false, err
	}
	if bind != nil {
		if bind.DramaCode == "" {
			return nil, false, fmt.Errorf("导入任务项绑定缺失短剧短码，拒绝自动重建")
		}
		if explicitTarget != "" && explicitTarget != bind.DramaCode {
			return nil, false, fmt.Errorf("导入项已绑定短剧 %s，与指定目标 %s 不一致", bind.DramaCode, explicitTarget)
		}
		got, err := getAssetByCodeDB(db, ctx, bind.DramaCode)
		if err != nil {
			return nil, false, err
		}
		if got == nil || !consts.IsDramaSeries(got.Kind, got.ParentId) {
			return nil, false, fmt.Errorf("导入任务已绑定的短剧不存在或已被删除，拒绝自动重建")
		}
		return got, false, nil
	}
	if explicitTarget != "" {
		got, err := getAssetByCodeDB(db, ctx, explicitTarget)
		if err != nil {
			return nil, false, err
		}
		if got == nil || !consts.IsDramaSeries(got.Kind, got.ParentId) {
			return nil, false, fmt.Errorf("指定的续传短剧不存在或不是短剧整部")
		}
		if err := insertDramaImportItemBindingDB(db, ctx, jobPK, key, got.Code, true); err != nil {
			return nil, false, err
		}
		return got, false, nil
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, false, errors.New("缺少标题或剧集")
	}
	code, err := createDramaDB(db, ctx, title, "", "", "", consts.UpdateStatusSerial)
	if err != nil {
		return nil, false, err
	}
	got, err := getAssetByCodeDB(db, ctx, code)
	if err != nil || got == nil {
		if err == nil {
			err = errors.New("创建短剧后无法读取")
		}
		return nil, false, err
	}
	if err := insertDramaImportItemBindingDB(db, ctx, jobPK, key, got.Code, false); err != nil {
		return nil, false, err
	}
	return got, true, nil
}
