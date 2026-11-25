package dao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

var (
	ErrDramaUploadAborted    = errors.New("上传会话已取消")
	ErrDramaUploadJobStarted = errors.New("导入任务已开始，不能取消上传会话")
	ErrDramaUploadNotFound   = errors.New("上传会话不存在")
	ErrDramaUploadNotReady   = errors.New("上传会话状态不允许完成")
)

func lockDramaUploadTx(ctx context.Context, tx gdb.TX, uploadID int64) (*DramaImportUpload, error) {
	row, err := tx.Model("drama_import_upload").Ctx(ctx).Where("id", uploadID).LockUpdate().One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, ErrDramaUploadNotFound
	}
	u := mapDramaUpload(row.Map())
	return &u, nil
}

func dramaJobByUploadTx(ctx context.Context, tx gdb.TX, uploadID int64, lock bool) (*DramaImportJob, error) {
	m := tx.Model("drama_import_job").Ctx(ctx).Where("upload_id", uploadID)
	if lock {
		m = m.LockUpdate()
	}
	row, err := m.One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	j := mapDramaJob(row.Map())
	return &j, nil
}

func GetDramaImportJobByUploadIDOnDB(db gdb.DB, ctx context.Context, uploadID int64) (*DramaImportJob, error) {
	row, err := db.Model("drama_import_job").Ctx(ctx).Where("upload_id", uploadID).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	j := mapDramaJob(row.Map())
	return &j, nil
}

// FinalizeDramaUploadOnDB 按上传行串行化：已取消拒绝，已有任务回填关联并幂等返回，否则建唯一任务并完成会话。
func FinalizeDramaUploadOnDB(db gdb.DB, ctx context.Context, uploadID int64, packKey, planJSON string) (job *DramaImportJob, created bool, err error) {
	err = db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		up, err := lockDramaUploadTx(ctx, tx, uploadID)
		if err != nil {
			return err
		}
		existing, err := dramaJobByUploadTx(ctx, tx, uploadID, false)
		if err != nil {
			return err
		}
		if up.Status == DramaUploadAborted {
			job = existing
			return ErrDramaUploadAborted
		}
		if existing != nil {
			job = existing
			pk := existing.PackKey
			if pk == "" {
				pk = packKey
			}
			_, err := tx.Model("drama_import_upload").Ctx(ctx).Where("id", uploadID).
				WhereNot("status", DramaUploadAborted).Data(g.Map{
				"job_code":     existing.Code,
				"pack_key":     pk,
				"plan_json":    existing.PlanJSON,
				"status":       DramaUploadCompleted,
				"completed_at": gdb.Raw("COALESCE(completed_at, clock_timestamp())"),
				"updated_at":   dbClock(),
			}).Update()
			return err
		}
		if up.Status != DramaUploadUploading && up.Status != DramaUploadCompleted {
			return ErrDramaUploadNotReady
		}
		var code string
		for i := 0; i < 8; i++ {
			c, err := newImportCode()
			if err != nil {
				return err
			}
			got, err := tx.Model("drama_import_job").Ctx(ctx).Where("code", c).One()
			if err != nil {
				return err
			}
			if got.IsEmpty() {
				code = c
				break
			}
		}
		if code == "" {
			return errors.New("生成导入任务短码失败, 请重试")
		}
		now := gtime.Now()
		id, err := tx.Model("drama_import_job").Ctx(ctx).Data(g.Map{
			"code":         code,
			"upload_id":    up.Pk,
			"upload_code":  up.Code,
			"bucket":       up.Bucket,
			"pack_key":     packKey,
			"plan_json":    planJSON,
			"status":       DramaJobQueued,
			"stage":        "queued",
			"max_attempts": 3,
			"created_at":   now,
			"updated_at":   now,
		}).InsertAndGetId()
		if err != nil {
			return err
		}
		r, err := tx.Model("drama_import_upload").Ctx(ctx).Where("id", uploadID).
			WhereIn("status", []string{DramaUploadUploading, DramaUploadCompleted}).Data(g.Map{
			"pack_key":     packKey,
			"plan_json":    planJSON,
			"job_code":     code,
			"status":       DramaUploadCompleted,
			"completed_at": dbClock(),
			"updated_at":   dbClock(),
		}).Update()
		if err != nil {
			return err
		}
		if n, _ := r.RowsAffected(); n != 1 {
			return ErrDramaUploadNotReady
		}
		job = &DramaImportJob{
			Pk: id, Code: code, UploadID: up.Pk, UploadCode: up.Code,
			Bucket: up.Bucket, PackKey: packKey, PlanJSON: planJSON,
			Status: DramaJobQueued, Stage: "queued", MaxAttempts: 3,
			CreatedAt: now.Time, UpdatedAt: now.Time,
		}
		created = true
		return nil
	})
	if err != nil && isUniqueViolation(err) {
		if j, e := GetDramaImportJobByUploadIDOnDB(db, ctx, uploadID); e == nil && j != nil {
			return j, false, nil
		}
	}
	if err != nil && !errors.Is(err, ErrDramaUploadAborted) {
		job, created = nil, false
	}
	return job, created, err
}

func FinalizeDramaUpload(ctx context.Context, uploadID int64, packKey, planJSON string) (*DramaImportJob, bool, error) {
	return FinalizeDramaUploadOnDB(g.DB(), ctx, uploadID, packKey, planJSON)
}

// AbortDramaUploadTxOnDB 先在上传行与任务行锁内取得取消资格并提交，调用方之后才可清理对象。
func AbortDramaUploadTxOnDB(db gdb.DB, ctx context.Context, uploadID int64, reason string) (already bool, err error) {
	err = db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		up, err := lockDramaUploadTx(ctx, tx, uploadID)
		if err != nil {
			return err
		}
		j, err := dramaJobByUploadTx(ctx, tx, uploadID, true)
		if err != nil {
			return err
		}
		if j != nil {
			switch j.Status {
			case DramaJobQueued, DramaJobFailed:
				r, err := tx.Model("drama_import_job").Ctx(ctx).Where("id", j.Pk).
					WhereIn("status", []string{DramaJobQueued, DramaJobFailed}).Data(g.Map{
					"status":           DramaJobCanceled,
					"stage":            "canceled",
					"message":          reason,
					"error":            reason,
					"owner":            "",
					"lease_until":      nil,
					"lease_generation": gdb.Raw("lease_generation + 1"),
					"finished_at":      dbClock(),
					"updated_at":       dbClock(),
				}).Update()
				if err != nil {
					return err
				}
				if n, _ := r.RowsAffected(); n != 1 {
					return ErrDramaUploadJobStarted
				}
			case DramaJobCanceled:
			default:
				return ErrDramaUploadJobStarted
			}
		}
		if up.Status == DramaUploadAborted {
			already = true
			return nil
		}
		_, err = tx.Model("drama_import_upload").Ctx(ctx).Where("id", uploadID).
			WhereNot("status", DramaUploadAborted).Data(g.Map{
			"status":     DramaUploadAborted,
			"updated_at": dbClock(),
		}).Update()
		return err
	})
	return already, err
}

// AbortExpiredDramaUploadOnDB 仅在行锁内确认仍为未建任务且已过宽限期的过期上传时标记取消。
func AbortExpiredDramaUploadOnDB(db gdb.DB, ctx context.Context, uploadID int64, grace time.Duration) (bool, error) {
	aborted := false
	err := db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		if _, err := lockDramaUploadTx(ctx, tx, uploadID); err != nil {
			return err
		}
		r, err := tx.Model("drama_import_upload").Ctx(ctx).
			Where("id", uploadID).
			Where("status", DramaUploadUploading).
			Where("job_code", "").
			Where(expiredPastGraceSQL(grace)).
			Where("NOT EXISTS (SELECT 1 FROM drama_import_job j WHERE j.upload_id = drama_import_upload.id)").
			Data(g.Map{"status": DramaUploadAborted, "updated_at": dbClock()}).Update()
		if err != nil {
			return err
		}
		n, _ := r.RowsAffected()
		aborted = n == 1
		return nil
	})
	return aborted, err
}

// expiredPastGraceSQL 过期判断统一用 DB 时钟并留出在途合并的宽限期。
func expiredPastGraceSQL(grace time.Duration) string {
	ms := grace.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	return fmt.Sprintf("expires_at + interval '%d milliseconds' < clock_timestamp()", ms)
}

func ListExpiredDramaUploadsOnDB(db gdb.DB, ctx context.Context, grace time.Duration, limit int) ([]DramaImportUpload, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := db.Model("drama_import_upload").Ctx(ctx).
		Where("status", DramaUploadUploading).
		Where(expiredPastGraceSQL(grace)).
		Where("job_code", "").
		OrderAsc("id").Limit(limit).All()
	if err != nil {
		return nil, err
	}
	out := make([]DramaImportUpload, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapDramaUpload(row.Map()))
	}
	return out, nil
}

// ListAbortedDramaUploadsForSweepOnDB 按 id 游标列出窗口内已取消的上传，供补扫迟到对象。
func ListAbortedDramaUploadsForSweepOnDB(db gdb.DB, ctx context.Context, afterID int64, window time.Duration, limit int) ([]DramaImportUpload, error) {
	if limit <= 0 {
		limit = 20
	}
	ms := window.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	rows, err := db.Model("drama_import_upload").Ctx(ctx).
		Where("status", DramaUploadAborted).
		Where(fmt.Sprintf("updated_at > clock_timestamp() - interval '%d milliseconds'", ms)).
		Where("id >", afterID).
		OrderAsc("id").Limit(limit).All()
	if err != nil {
		return nil, err
	}
	out := make([]DramaImportUpload, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapDramaUpload(row.Map()))
	}
	return out, nil
}
