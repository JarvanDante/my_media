package dao

import (
	"context"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"

	"github.com/JarvanDante/my_media/internal/shared/authz"
)

type Client struct {
	Id           int64
	AppKey       string
	AppSecret    string
	SecretHashed int
	SiteCode     string
	Status       int
	Remark       string
	CreatedAt    string
	UpdatedAt    string
}

type ClientRepo struct{}

func NewClientRepo() *ClientRepo { return &ClientRepo{} }

func (r *ClientRepo) List(ctx context.Context, page, size int, keyword string) ([]Client, int, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	m := g.DB().Model("paas_client").Ctx(ctx).Safe()
	if keyword != "" {
		kw := "%" + keyword + "%"
		m = m.Where("(app_key LIKE ? OR site_code LIKE ?)", kw, kw)
	}
	total, err := m.Clone().Count()
	if err != nil {
		return nil, 0, err
	}
	rows, err := m.OrderDesc("id").Page(page, size).All()
	if err != nil {
		return nil, 0, err
	}
	list := make([]Client, 0, len(rows))
	for _, row := range rows {
		list = append(list, mapClient(row.Map()))
	}
	return list, total, nil
}

func (r *ClientRepo) Upsert(ctx context.Context, appKey, plainSecret, siteCode, remark string, status int) error {
	if status != 0 && status != 1 {
		status = 1
	}
	hash := authz.HashSecret(plainSecret)
	exist, err := g.DB().Model("paas_client").Ctx(ctx).Where("app_key", appKey).One()
	if err != nil {
		return err
	}
	data := g.Map{
		"app_secret":    hash,
		"secret_hashed": 1,
		"site_code":     siteCode,
		"status":        status,
		"remark":        remark,
		"updated_at":    gtime.Now(),
	}
	if exist.IsEmpty() {
		data["app_key"] = appKey
		data["created_at"] = gtime.Now()
		_, err = g.DB().Model("paas_client").Ctx(ctx).Data(data).Insert()
		return err
	}
	_, err = g.DB().Model("paas_client").Ctx(ctx).Where("app_key", appKey).Data(data).Update()
	return err
}

func (r *ClientRepo) Disable(ctx context.Context, appKey string) error {
	_, err := g.DB().Model("paas_client").Ctx(ctx).Where("app_key", appKey).Data(g.Map{
		"status": 0, "updated_at": gtime.Now(),
	}).Update()
	return err
}

func (r *ClientRepo) FindActive(ctx context.Context, appKey string) (*Client, error) {
	row, err := g.DB().Model("paas_client").Ctx(ctx).
		Where("app_key", appKey).Where("status", 1).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	c := mapClient(row.Map())
	return &c, nil
}

func (r *ClientRepo) GetByAppKey(ctx context.Context, appKey string) (*Client, error) {
	row, err := g.DB().Model("paas_client").Ctx(ctx).Where("app_key", appKey).One()
	if err != nil {
		return nil, err
	}
	if row.IsEmpty() {
		return nil, nil
	}
	c := mapClient(row.Map())
	return &c, nil
}

func mapClient(m g.Map) Client {
	return Client{
		Id:           g.NewVar(m["id"]).Int64(),
		AppKey:       g.NewVar(m["app_key"]).String(),
		AppSecret:    g.NewVar(m["app_secret"]).String(),
		SecretHashed: g.NewVar(m["secret_hashed"]).Int(),
		SiteCode:     g.NewVar(m["site_code"]).String(),
		Status:       g.NewVar(m["status"]).Int(),
		Remark:       g.NewVar(m["remark"]).String(),
		CreatedAt:    g.NewVar(m["created_at"]).String(),
		UpdatedAt:    g.NewVar(m["updated_at"]).String(),
	}
}
