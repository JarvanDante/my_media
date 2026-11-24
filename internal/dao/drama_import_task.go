package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/consts"
	"github.com/JarvanDante/my_media/internal/shared/kit"
)

const (
	DramaUploadUploading = "uploading"
	DramaUploadCompleted = "completed"
	DramaUploadAborted   = "aborted"

	DramaJobQueued     = "queued"
	DramaJobRunning    = "running"
	DramaJobImportDone = "import_done"
	DramaJobSucceeded  = "succeeded"
	DramaJobFailed     = "failed"
	DramaJobCanceled   = "canceled"
)

type DramaImportPart struct {
	N          int
	Size       int64
	ObjectKey  string
	UploadedAt *time.Time
}

type DramaImportUpload struct {
	Pk          int64
	Code        string
	Filename    string
	TotalSize   int64
	ChunkSize   int64
	PartCount   int
	Bucket      string
	Prefix      string
	PackKey     string
	Status      string
	JobCode     string
	PlanJSON    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ExpiresAt   time.Time
	CompletedAt *time.Time
	Parts       []DramaImportPart
}

type DramaImportJob struct {
	Pk              int64
	Code            string
	UploadID        int64
	UploadCode      string
	Bucket          string
	PackKey         string
	PlanJSON        string
	Status          string
	Stage           string
	Progress        int
	Message         string
	Error           string
	ResultJSON      string
	Attempts        int
	MaxAttempts     int
	Owner           string
	LeaseGeneration int64
	LeaseUntil      *time.Time
	HeartbeatAt     *time.Time
	StartedAt       *time.Time
	FinishedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// DramaJobLease 一次成功认领后的执行租约，所有进度/完成/清理必须带上 fencing。
type DramaJobLease struct {
	JobPK      int64
	Owner      string
	Generation int64
}

const (
	// DefaultDramaJobLeaseTTL 执行租约时长；周期续租、关闭释放后的可回收时刻与误回收判断共用此阈值。
	DefaultDramaJobLeaseTTL = 30 * time.Second
	// dramaImportClaimLockKey 认领短事务内的 PG 事务级咨询锁，用于配额与选任务原子化。
	dramaImportClaimLockKey int64 = 8520140916
)

// ErrDramaJobLeaseLost 当前 owner/generation 已不再持有该任务的执行租约。
var ErrDramaJobLeaseLost = errors.New("drama import job lease lost")

func newImportCode() (string, error) {
	for i := 0; i < 8; i++ {
		c, err := kit.NewPublicID()
		if err != nil {
			return "", err
		}
		if ValidDramaImportCode(c) {
			return c, nil
		}
	}
	return "", errors.New("生成任务短码失败, 请重试")
}

func ValidDramaImportCode(code string) bool {
	if len(code) < 8 || len(code) > 16 {
		return false
	}
	for _, r := range code {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func CreateDramaUpload(ctx context.Context, filename string, total, chunk int64, partCount int, bucket string, expire time.Duration) (*DramaImportUpload, error) {
	var out *DramaImportUpload
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		var code string
		for i := 0; i < 8; i++ {
			c, err := newImportCode()
			if err != nil {
				return err
			}
			exist, err := tx.Model("drama_import_upload").Ctx(ctx).Where("code", c).One()
			if err != nil {
				return err
			}
			if exist.IsEmpty() {
				code = c
				break
			}
		}
		if code == "" {
			return errors.New("生成上传会话短码失败, 请重试")
		}
		pref := consts.DramaImportPrefix(code)
		now := gtime.Now()
		exp := gtime.NewFromTime(time.Now().Add(expire))
		id, err := tx.Model("drama_import_upload").Ctx(ctx).Data(g.Map{
			"code":       code,
			"filename":   filename,
			"total_size": total,
			"chunk_size": chunk,
			"part_count": partCount,
			"bucket":     bucket,
			"prefix":     pref,
			"status":     DramaUploadUploading,
			"created_at": now,
			"updated_at": now,
			"expires_at": exp,
		}).InsertAndGetId()
		if err != nil {
			return err
		}
		parts := make([]DramaImportPart, 0, partCount)
		remain := total
		for n := 1; n <= partCount; n++ {
			sz := chunk
			if remain < chunk {
				sz = remain
			}
			if n == partCount {
				sz = remain
			}
			if sz < 0 {
				sz = 0
			}
			key := pref + partObjectName(n)
			if _, err := tx.Model("drama_import_part").Ctx(ctx).Data(g.Map{
				"upload_id":  id,
				"n":          n,
				"size":       sz,
				"object_key": key,
			}).Insert(); err != nil {
				return err
			}
			parts = append(parts, DramaImportPart{N: n, Size: sz, ObjectKey: key})
			remain -= sz
		}
		out = &DramaImportUpload{
			Pk: id, Code: code, Filename: filename, TotalSize: total, ChunkSize: chunk,
			PartCount: partCount, Bucket: bucket, Prefix: pref, Status: DramaUploadUploading,
			CreatedAt: now.Time, UpdatedAt: now.Time, ExpiresAt: exp.Time, Parts: parts,
		}
		return nil
	})
	return out, err
}

func partObjectName(n int) string {
	return "part-" + pad5(n)
}

func pad5(n int) string {
	s := []byte("00000")
	v := n
	for i := 4; i >= 0; i-- {
		s[i] = byte('0' + v%10)
		v /= 10
	}
	return string(s)
}

func GetDramaUploadByCode(ctx context.Context, code string) (*DramaImportUpload, error) {
	if !ValidDramaImportCode(code) {
		return nil, nil
	}
	row, err := g.DB().Model("drama_import_upload").Ctx(ctx).Where("code", code).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	u := mapDramaUpload(row.Map())
	parts, err := listDramaParts(ctx, u.Pk)
	if err != nil {
		return nil, err
	}
	u.Parts = parts
	return &u, nil
}

func listDramaParts(ctx context.Context, uploadID int64) ([]DramaImportPart, error) {
	rows, err := g.DB().Model("drama_import_part").Ctx(ctx).Where("upload_id", uploadID).OrderAsc("n").All()
	if err != nil {
		return nil, err
	}
	out := make([]DramaImportPart, 0, len(rows))
	for _, row := range rows {
		m := row.Map()
		p := DramaImportPart{
			N:         toInt(m["n"]),
			Size:      toInt64(m["size"]),
			ObjectKey: toStr(m["object_key"]),
		}
		if t := toTime(m["uploaded_at"]); t != nil {
			p.UploadedAt = t
		}
		out = append(out, p)
	}
	return out, nil
}

func MarkDramaPartUploaded(ctx context.Context, uploadID int64, n int, size int64) error {
	_, err := g.DB().Model("drama_import_part").Ctx(ctx).
		Where("upload_id", uploadID).Where("n", n).Data(g.Map{
		"size":        size,
		"uploaded_at": gtime.Now(),
	}).Update()
	return err
}

func MarkDramaPartsUploaded(ctx context.Context, uploadID int64, ns []int) error {
	if len(ns) == 0 {
		return nil
	}
	_, err := g.DB().Model("drama_import_part").Ctx(ctx).
		Where("upload_id", uploadID).WhereIn("n", ns).Data(g.Map{
		"uploaded_at": gtime.Now(),
	}).Update()
	return err
}

func CompleteDramaUpload(ctx context.Context, uploadID int64, packKey, planJSON, jobCode string) error {
	_, err := g.DB().Model("drama_import_upload").Ctx(ctx).Where("id", uploadID).Data(g.Map{
		"pack_key":     packKey,
		"plan_json":    planJSON,
		"job_code":     jobCode,
		"status":       DramaUploadCompleted,
		"completed_at": gtime.Now(),
		"updated_at":   gtime.Now(),
	}).Update()
	return err
}

func AbortDramaUpload(ctx context.Context, uploadID int64) error {
	_, err := g.DB().Model("drama_import_upload").Ctx(ctx).Where("id", uploadID).Data(g.Map{
		"status":     DramaUploadAborted,
		"updated_at": gtime.Now(),
	}).Update()
	return err
}

func ListExpiredDramaUploads(ctx context.Context, limit int) ([]DramaImportUpload, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := g.DB().Model("drama_import_upload").Ctx(ctx).
		Where("status", DramaUploadUploading).
		Where("expires_at <", gtime.Now()).
		Where("job_code", "").
		Limit(limit).All()
	if err != nil {
		return nil, err
	}
	out := make([]DramaImportUpload, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapDramaUpload(row.Map()))
	}
	return out, nil
}

func CreateDramaImportJob(ctx context.Context, upload *DramaImportUpload, planJSON string) (*DramaImportJob, error) {
	if upload == nil {
		return nil, errors.New("缺少上传会话")
	}
	var out *DramaImportJob
	err := g.DB().Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		// 同一会话只建一个任务（幂等）
		exist, err := tx.Model("drama_import_job").Ctx(ctx).Where("upload_id", upload.Pk).One()
		if err != nil {
			return err
		}
		if !exist.IsEmpty() {
			j := mapDramaJob(exist.Map())
			out = &j
			return nil
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
			"upload_id":    upload.Pk,
			"upload_code":  upload.Code,
			"bucket":       upload.Bucket,
			"pack_key":     upload.PackKey,
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
		if _, err := tx.Model("drama_import_upload").Ctx(ctx).Where("id", upload.Pk).Data(g.Map{
			"job_code":   code,
			"plan_json":  planJSON,
			"updated_at": now,
		}).Update(); err != nil {
			return err
		}
		out = &DramaImportJob{
			Pk: id, Code: code, UploadID: upload.Pk, UploadCode: upload.Code,
			Bucket: upload.Bucket, PackKey: upload.PackKey, PlanJSON: planJSON,
			Status: DramaJobQueued, Stage: "queued", MaxAttempts: 3,
			CreatedAt: now.Time, UpdatedAt: now.Time,
		}
		return nil
	})
	return out, err
}

func GetDramaImportJobByCode(ctx context.Context, code string) (*DramaImportJob, error) {
	if !ValidDramaImportCode(code) {
		return nil, nil
	}
	row, err := g.DB().Model("drama_import_job").Ctx(ctx).Where("code", code).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	j := mapDramaJob(row.Map())
	return &j, nil
}

func DramaJobLeaseHeld(status string, until *time.Time, now time.Time) bool {
	return status == DramaJobRunning && until != nil && until.After(now)
}

func DramaJobClaimable(status string, until *time.Time, attempts, maxAttempts int, now time.Time) bool {
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	if attempts >= maxAttempts {
		return false
	}
	if status == DramaJobQueued {
		return true
	}
	if status == DramaJobRunning && !DramaJobLeaseHeld(status, until, now) {
		return true
	}
	return false
}

func DramaJobLeaseFenced(jobOwner, reqOwner string, jobGen, reqGen int64, status string, until *time.Time, now time.Time) bool {
	return jobOwner != "" && jobOwner == reqOwner && jobGen == reqGen && DramaJobLeaseHeld(status, until, now)
}

// DramaJobLeaseCallTimeout Renew/Release/Finish 等租约 RPC 的上限，避免卡住的调用拖住业务与关闭。
func DramaJobLeaseCallTimeout(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		ttl = DefaultDramaJobLeaseTTL
	}
	d := ttl / 3
	if d < 50*time.Millisecond {
		d = 50 * time.Millisecond
	}
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

func leaseUntilSQL(ttl time.Duration) gdb.Raw {
	if ttl <= 0 {
		ttl = DefaultDramaJobLeaseTTL
	}
	ms := ttl.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	return gdb.Raw(fmt.Sprintf("now() + interval '%d milliseconds'", ms))
}

func CountRunningDramaJobs(ctx context.Context) (int, error) {
	return countLeasedRunningJobs(g.DB(), ctx)
}

func countLeasedRunningJobs(db gdb.DB, ctx context.Context) (int, error) {
	n, err := db.Model("drama_import_job").Ctx(ctx).
		Where("status", DramaJobRunning).
		Where("lease_until IS NOT NULL").
		Where("lease_until > now()").
		Count()
	return n, err
}

func ClaimDramaImportJob(ctx context.Context, owner string, leaseTTL time.Duration, maxRunning int) (*DramaImportJob, *DramaJobLease, error) {
	return claimDramaImportJobDB(g.DB(), ctx, owner, leaseTTL, maxRunning)
}

func claimDramaImportJobDB(db gdb.DB, ctx context.Context, owner string, leaseTTL time.Duration, maxRunning int) (*DramaImportJob, *DramaJobLease, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return nil, nil, errors.New("missing job lease owner")
	}
	if len(owner) > 64 {
		owner = owner[:64]
	}
	if leaseTTL <= 0 {
		leaseTTL = DefaultDramaJobLeaseTTL
	}
	if maxRunning <= 0 {
		maxRunning = 1
	}
	var out *DramaImportJob
	var lease *DramaJobLease
	err := db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		if _, err := tx.Exec("SELECT pg_advisory_xact_lock(?)", dramaImportClaimLockKey); err != nil {
			return err
		}
		n, err := tx.Model("drama_import_job").Ctx(ctx).
			Where("status", DramaJobRunning).
			Where("lease_until IS NOT NULL").
			Where("lease_until > now()").
			Count()
		if err != nil {
			return err
		}
		if n >= maxRunning {
			return nil
		}
		now := gtime.Now()
		row, err := tx.Model("drama_import_job").Ctx(ctx).
			Where("attempts < max_attempts").
			Where("(status = ? OR (status = ? AND (lease_until IS NULL OR lease_until <= now())))", DramaJobQueued, DramaJobRunning).
			OrderAsc("id").
			LockUpdate().
			Limit(1).
			One()
		if err != nil {
			return err
		}
		if row.IsEmpty() {
			return nil
		}
		j := mapDramaJob(row.Map())
		started := now
		if j.StartedAt != nil {
			started = gtime.NewFromTime(*j.StartedAt)
		}
		nextGen := j.LeaseGeneration + 1
		r, err := tx.Model("drama_import_job").Ctx(ctx).
			Where("id", j.Pk).
			Where("lease_generation", j.LeaseGeneration).
			Where("attempts < max_attempts").
			Where("(status = ? OR (status = ? AND (lease_until IS NULL OR lease_until <= now())))", DramaJobQueued, DramaJobRunning).
			Data(g.Map{
				"status":           DramaJobRunning,
				"stage":            "running",
				"attempts":         j.Attempts + 1,
				"owner":            owner,
				"lease_generation": nextGen,
				"lease_until":      leaseUntilSQL(leaseTTL),
				"heartbeat_at":     gdb.Raw("now()"),
				"started_at":       started,
				"updated_at":       gdb.Raw("now()"),
				"finished_at":      nil,
				"error":            "",
			}).Update()
		if err != nil {
			return err
		}
		affected, _ := r.RowsAffected()
		if affected == 0 {
			return nil
		}
		j.Status = DramaJobRunning
		j.Stage = "running"
		j.Attempts = j.Attempts + 1
		j.Owner = owner
		j.LeaseGeneration = nextGen
		approx := time.Now().Add(leaseTTL)
		j.LeaseUntil = &approx
		ht := now.Time
		j.HeartbeatAt = &ht
		if j.StartedAt == nil {
			j.StartedAt = &ht
		}
		out = &j
		lease = &DramaJobLease{JobPK: j.Pk, Owner: owner, Generation: nextGen}
		return nil
	})
	return out, lease, err
}

func leaseFenceWhere(m *gdb.Model, lease DramaJobLease) *gdb.Model {
	return m.Where("id", lease.JobPK).
		Where("owner", lease.Owner).
		Where("lease_generation", lease.Generation).
		Where("status", DramaJobRunning).
		Where("lease_until IS NOT NULL").
		Where("lease_until > now()")
}

func rowsOrLeaseLost(r sqlResult, err error) error {
	if err != nil {
		return err
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrDramaJobLeaseLost
	}
	return nil
}

type sqlResult interface {
	RowsAffected() (int64, error)
}

func RenewDramaImportJobLease(ctx context.Context, lease DramaJobLease, ttl time.Duration) error {
	return renewDramaImportJobLeaseDB(g.DB(), ctx, lease, ttl)
}

func renewDramaImportJobLeaseDB(db gdb.DB, ctx context.Context, lease DramaJobLease, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = DefaultDramaJobLeaseTTL
	}
	r, err := leaseFenceWhere(db.Model("drama_import_job").Ctx(ctx), lease).Data(g.Map{
		"lease_until":  leaseUntilSQL(ttl),
		"heartbeat_at": gdb.Raw("now()"),
		"updated_at":   gdb.Raw("now()"),
	}).Update()
	return rowsOrLeaseLost(r, err)
}

func HeartbeatDramaImportJob(ctx context.Context, lease DramaJobLease, stage string, progress int, message string) error {
	return heartbeatDramaImportJobDB(g.DB(), ctx, lease, stage, progress, message, DefaultDramaJobLeaseTTL)
}

func heartbeatDramaImportJobDB(db gdb.DB, ctx context.Context, lease DramaJobLease, stage string, progress int, message string, ttl time.Duration) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	if ttl <= 0 {
		ttl = DefaultDramaJobLeaseTTL
	}
	data := g.Map{
		"lease_until":  leaseUntilSQL(ttl),
		"heartbeat_at": gdb.Raw("now()"),
		"updated_at":   gdb.Raw("now()"),
		"progress":     progress,
	}
	if strings.TrimSpace(stage) != "" {
		data["stage"] = stage
	}
	if message != "" {
		data["message"] = message
	}
	r, err := leaseFenceWhere(db.Model("drama_import_job").Ctx(ctx), lease).Data(data).Update()
	return rowsOrLeaseLost(r, err)
}

func FinishDramaImportJob(ctx context.Context, lease DramaJobLease, status, stage, message, errMsg, resultJSON string, progress int) error {
	return finishDramaImportJobDB(g.DB(), ctx, lease, status, stage, message, errMsg, resultJSON, progress)
}

func finishDramaImportJobDB(db gdb.DB, ctx context.Context, lease DramaJobLease, status, stage, message, errMsg, resultJSON string, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	r, err := leaseFenceWhere(db.Model("drama_import_job").Ctx(ctx), lease).Data(g.Map{
		"status":           status,
		"stage":            stage,
		"message":          message,
		"error":            errMsg,
		"result_json":      resultJSON,
		"progress":         progress,
		"finished_at":      gdb.Raw("now()"),
		"updated_at":       gdb.Raw("now()"),
		"heartbeat_at":     gdb.Raw("now()"),
		"owner":            "",
		"lease_generation": gdb.Raw("lease_generation + 1"),
		"lease_until":      nil,
	}).Update()
	return rowsOrLeaseLost(r, err)
}

func ReleaseDramaImportJobLease(ctx context.Context, lease DramaJobLease) error {
	return releaseDramaImportJobLeaseDB(g.DB(), ctx, lease)
}

func releaseDramaImportJobLeaseDB(db gdb.DB, ctx context.Context, lease DramaJobLease) error {
	// 使 (owner, generation) 立即失效，并清空到期时间；回收条件仍是无有效租约。
	r, err := leaseFenceWhere(db.Model("drama_import_job").Ctx(ctx), lease).Data(g.Map{
		"owner":            "",
		"lease_generation": gdb.Raw("lease_generation + 1"),
		"lease_until":      nil,
		"heartbeat_at":     gdb.Raw("now()"),
		"updated_at":       gdb.Raw("now()"),
	}).Update()
	return rowsOrLeaseLost(r, err)
}

func assertDramaJobLeaseTx(ctx context.Context, tx gdb.TX, lease DramaJobLease) error {
	row, err := leaseFenceWhere(tx.Model("drama_import_job").Ctx(ctx), lease).Fields("id").LockUpdate().One()
	if err != nil {
		return err
	}
	if row.IsEmpty() {
		return ErrDramaJobLeaseLost
	}
	return nil
}

// AssertDramaJobLease 短事务校验租约仍有效（不含业务写入）。
func AssertDramaJobLease(ctx context.Context, lease DramaJobLease) error {
	return assertDramaJobLeaseDB(g.DB(), ctx, lease)
}

func assertDramaJobLeaseDB(db gdb.DB, ctx context.Context, lease DramaJobLease) error {
	return db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		return assertDramaJobLeaseTx(ctx, tx, lease)
	})
}

// WithDramaJobLease 在同一短事务中校验有效租约并执行 DB 副作用；不得包裹 MinIO/Kafka。
func WithDramaJobLease(ctx context.Context, lease DramaJobLease, fn func(ctx context.Context) error) error {
	return withDramaJobLeaseDB(g.DB(), ctx, lease, fn)
}

func withDramaJobLeaseDB(db gdb.DB, ctx context.Context, lease DramaJobLease, fn func(ctx context.Context) error) error {
	if fn == nil {
		return errors.New("missing lease callback")
	}
	return db.Transaction(ctx, func(ctx context.Context, tx gdb.TX) error {
		if err := assertDramaJobLeaseTx(ctx, tx, lease); err != nil {
			return err
		}
		return fn(ctx)
	})
}

func CancelQueuedDramaImportJob(ctx context.Context, id int64, message string) error {
	now := gtime.Now()
	_, err := g.DB().Model("drama_import_job").Ctx(ctx).
		Where("id", id).
		Where("status", DramaJobQueued).
		Data(g.Map{
			"status":      DramaJobCanceled,
			"stage":       "canceled",
			"message":     message,
			"error":       message,
			"finished_at": now,
			"updated_at":  now,
			"owner":       "",
			"lease_until": nil,
		}).Update()
	return err
}

func RequeueDramaImportJob(ctx context.Context, id int64) error {
	_, err := g.DB().Model("drama_import_job").Ctx(ctx).
		Where("id", id).
		WhereIn("status", []string{DramaJobFailed, DramaJobCanceled}).
		Data(g.Map{
			"status":      DramaJobQueued,
			"stage":       "queued",
			"error":       "",
			"attempts":    0,
			"finished_at": nil,
			"updated_at":  gtime.Now(),
			"owner":       "",
			"lease_until": nil,
		}).Update()
	return err
}

func UpdateDramaImportJobResult(ctx context.Context, id int64, status, stage, message, resultJSON string, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 100 {
		progress = 100
	}
	// import_done 之后的转码进度刷新不属于执行租约；禁止回写 running。
	_, err := g.DB().Model("drama_import_job").Ctx(ctx).Where("id", id).
		Where("status", DramaJobImportDone).
		Data(g.Map{
			"status":      status,
			"stage":       stage,
			"message":     message,
			"result_json": resultJSON,
			"progress":    progress,
			"updated_at":  gtime.Now(),
		}).Update()
	return err
}

func FailExhaustedStaleDramaJobs(ctx context.Context) error {
	_, err := g.DB().Model("drama_import_job").Ctx(ctx).
		Where("status", DramaJobRunning).
		Where("(lease_until IS NULL OR lease_until <= now())").
		Where("attempts >= max_attempts").
		Data(g.Map{
			"status":           DramaJobFailed,
			"stage":            "failed",
			"error":            "任务中断且已达最大重试次数",
			"message":          "任务中断且已达最大重试次数",
			"finished_at":      gdb.Raw("now()"),
			"updated_at":       gdb.Raw("now()"),
			"owner":            "",
			"lease_generation": gdb.Raw("lease_generation + 1"),
			"lease_until":      nil,
		}).Update()
	return err
}

func ListImportDoneDramaJobs(ctx context.Context, limit int) ([]DramaImportJob, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := g.DB().Model("drama_import_job").Ctx(ctx).
		Where("status", DramaJobImportDone).
		OrderAsc("id").Limit(limit).All()
	if err != nil {
		return nil, err
	}
	out := make([]DramaImportJob, 0, len(rows))
	for _, row := range rows {
		out = append(out, mapDramaJob(row.Map()))
	}
	return out, nil
}

type EpisodeStatusCount struct {
	Total      int
	Ready      int
	Processing int
	Failed     int
	Draft      int
}

func CountEpisodesByParent(ctx context.Context, parentPK int64) (EpisodeStatusCount, error) {
	var c EpisodeStatusCount
	if parentPK <= 0 {
		return c, nil
	}
	rows, err := g.DB().Model("media_asset").Ctx(ctx).
		Where("parent_id", parentPK).Fields("status").All()
	if err != nil {
		return c, err
	}
	for _, row := range rows {
		c.Total++
		switch toInt(row.Map()["status"]) {
		case consts.AssetStatusReady:
			c.Ready++
		case consts.AssetStatusProcessing:
			c.Processing++
		case consts.AssetStatusFailed:
			c.Failed++
		default:
			c.Draft++
		}
	}
	return c, nil
}

func mapDramaUpload(m map[string]interface{}) DramaImportUpload {
	u := DramaImportUpload{
		Pk: toInt64(m["id"]), Code: toStr(m["code"]), Filename: toStr(m["filename"]),
		TotalSize: toInt64(m["total_size"]), ChunkSize: toInt64(m["chunk_size"]),
		PartCount: toInt(m["part_count"]), Bucket: toStr(m["bucket"]), Prefix: toStr(m["prefix"]),
		PackKey: toStr(m["pack_key"]), Status: toStr(m["status"]), JobCode: toStr(m["job_code"]),
		PlanJSON: toStr(m["plan_json"]),
	}
	if t := toTime(m["created_at"]); t != nil {
		u.CreatedAt = *t
	}
	if t := toTime(m["updated_at"]); t != nil {
		u.UpdatedAt = *t
	}
	if t := toTime(m["expires_at"]); t != nil {
		u.ExpiresAt = *t
	}
	u.CompletedAt = toTime(m["completed_at"])
	return u
}

func mapDramaJob(m map[string]interface{}) DramaImportJob {
	j := DramaImportJob{
		Pk: toInt64(m["id"]), Code: toStr(m["code"]), UploadID: toInt64(m["upload_id"]),
		UploadCode: toStr(m["upload_code"]), Bucket: toStr(m["bucket"]), PackKey: toStr(m["pack_key"]),
		PlanJSON: toStr(m["plan_json"]), Status: toStr(m["status"]), Stage: toStr(m["stage"]),
		Progress: toInt(m["progress"]), Message: toStr(m["message"]), Error: toStr(m["error"]),
		ResultJSON: toStr(m["result_json"]), Attempts: toInt(m["attempts"]),
		MaxAttempts: toInt(m["max_attempts"]), Owner: toStr(m["owner"]),
		LeaseGeneration: toInt64(m["lease_generation"]),
	}
	j.LeaseUntil = toTime(m["lease_until"])
	j.HeartbeatAt = toTime(m["heartbeat_at"])
	j.StartedAt = toTime(m["started_at"])
	j.FinishedAt = toTime(m["finished_at"])
	if t := toTime(m["created_at"]); t != nil {
		j.CreatedAt = *t
	}
	if t := toTime(m["updated_at"]); t != nil {
		j.UpdatedAt = *t
	}
	if j.MaxAttempts <= 0 {
		j.MaxAttempts = 3
	}
	return j
}

func toStr(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return strings.TrimSpace(g.NewVar(v).String())
	}
}

func toInt(v interface{}) int { return int(toInt64(v)) }

func toInt64(v interface{}) int64 {
	if v == nil {
		return 0
	}
	return g.NewVar(v).Int64()
}

func toTime(v interface{}) *time.Time {
	if v == nil {
		return nil
	}
	if t, ok := v.(*gtime.Time); ok {
		if t == nil || t.IsZero() {
			return nil
		}
		tt := t.Time
		return &tt
	}
	if t, ok := v.(time.Time); ok {
		if t.IsZero() {
			return nil
		}
		return &t
	}
	gt := gtime.NewFromStr(g.NewVar(v).String())
	if gt == nil || gt.IsZero() {
		return nil
	}
	tt := gt.Time
	return &tt
}
