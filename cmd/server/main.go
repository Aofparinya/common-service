package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/spf13/viper"
	thaiaddress "github.com/ultramcu/go-thaiaddress"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type user struct {
	Sub         string   `json:"sub"`
	Type        string   `json:"type"`
	Permissions []string `json:"permissions"`
}

func (u user) has(p string) bool {
	for _, v := range u.Permissions {
		if v == p {
			return true
		}
	}
	return false
}

type masterType struct {
	ID          uuid.UUID `json:"id" gorm:"type:uuid;primaryKey"`
	Code        string    `json:"code" gorm:"uniqueIndex"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	IsActive    bool      `json:"isActive"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func (masterType) TableName() string { return "common.master_data_types" }

type masterItem struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey"`
	TypeID    uuid.UUID      `json:"-"`
	Code      string         `json:"code"`
	Name      string         `json:"name"`
	Value     map[string]any `json:"value" gorm:"serializer:json"`
	SortOrder int            `json:"sortOrder"`
	IsActive  bool           `json:"isActive"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `json:"-"`
}

func (masterItem) TableName() string { return "common.master_data_items" }

type setting struct {
	Key         string     `json:"key" gorm:"primaryKey"`
	Value       any        `json:"value" gorm:"serializer:json"`
	Description *string    `json:"description,omitempty"`
	UpdatedBy   *uuid.UUID `json:"updatedBy,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func (setting) TableName() string { return "common.system_configs" }

type flag struct {
	Key         string     `json:"key" gorm:"primaryKey"`
	Enabled     bool       `json:"enabled"`
	Description *string    `json:"description,omitempty"`
	UpdatedBy   *uuid.UUID `json:"updatedBy,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func (flag) TableName() string { return "common.feature_flags" }

type thaiProvince struct {
	Code       int    `json:"code" gorm:"primaryKey"`
	NameTH     string `json:"nameTh" gorm:"column:name_th"`
	NameEN     string `json:"nameEn" gorm:"column:name_en"`
	RegionCode int    `json:"regionCode" gorm:"column:region_code"`
}

func (thaiProvince) TableName() string { return "common.thai_provinces" }

type thaiDistrict struct {
	Code         int    `json:"code" gorm:"primaryKey"`
	ProvinceCode int    `json:"provinceCode" gorm:"column:province_code"`
	NameTH       string `json:"nameTh" gorm:"column:name_th"`
	NameEN       string `json:"nameEn" gorm:"column:name_en"`
}

func (thaiDistrict) TableName() string { return "common.thai_districts" }

type thaiSubdistrict struct {
	Code         int    `json:"code" gorm:"primaryKey"`
	DistrictCode int    `json:"districtCode" gorm:"column:district_code"`
	ProvinceCode int    `json:"provinceCode" gorm:"column:province_code"`
	NameTH       string `json:"nameTh" gorm:"column:name_th"`
	NameEN       string `json:"nameEn" gorm:"column:name_en"`
	PostalCode   string `json:"postalCode" gorm:"column:postal_code"`
}

func (thaiSubdistrict) TableName() string { return "common.thai_subdistricts" }

type thaiLocation struct {
	ProvinceCode      int    `json:"provinceCode"`
	ProvinceNameTH    string `json:"provinceNameTh"`
	ProvinceNameEN    string `json:"provinceNameEn"`
	DistrictCode      int    `json:"districtCode"`
	DistrictNameTH    string `json:"districtNameTh"`
	DistrictNameEN    string `json:"districtNameEn"`
	SubdistrictCode   int    `json:"subdistrictCode"`
	SubdistrictNameTH string `json:"subdistrictNameTh"`
	SubdistrictNameEN string `json:"subdistrictNameEn"`
	PostalCode        string `json:"postalCode"`
}

type typeInput struct {
	Code        string  `json:"code" validate:"required,uppercase,alphanum"`
	Name        string  `json:"name" validate:"required,max=150"`
	Description *string `json:"description"`
}
type itemInput struct {
	Code      string         `json:"code" validate:"required,max=100"`
	Name      string         `json:"name" validate:"required,max=255"`
	Value     map[string]any `json:"value"`
	SortOrder int            `json:"sortOrder"`
	IsActive  *bool          `json:"isActive"`
}
type valueInput struct {
	Value       any     `json:"value" validate:"required"`
	Description *string `json:"description"`
}
type flagInput struct {
	Enabled     bool    `json:"enabled"`
	Description *string `json:"description"`
}
type numberInput struct {
	DocumentType string `json:"documentType" validate:"required,oneof=CUS PRD ORD PAY INV REF"`
}
type validation struct{ v *validator.Validate }

func (v validation) Validate(value any) error { return v.v.Struct(value) }

func main() {
	if err := run(); err != nil {
		slog.Error("common service stopped", "error", err)
		os.Exit(1)
	}
}
func run() error {
	viper.AutomaticEnv()
	viper.SetDefault("PORT", 3006)
	dsn := strings.TrimSpace(viper.GetString("DATABASE_URL"))
	auth := strings.TrimRight(strings.TrimSpace(viper.GetString("AUTH_SERVICE_URL")), "/")
	if dsn == "" || auth == "" {
		return errors.New("DATABASE_URL and AUTH_SERVICE_URL are required")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	if err = migrate(db); err != nil {
		return err
	}
	app := server(db, auth)
	go func() {
		if err := app.Start(fmt.Sprintf(":%d", viper.GetInt("PORT"))); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return app.Shutdown(ctx)
}
func migrate(db *gorm.DB) error {
	for _, sql := range []string{
		`CREATE SCHEMA IF NOT EXISTS common`,
		`CREATE TABLE IF NOT EXISTS common.master_data_types(id UUID PRIMARY KEY,code VARCHAR(100) UNIQUE NOT NULL,name VARCHAR(150) NOT NULL,description TEXT,is_active BOOLEAN NOT NULL DEFAULT TRUE,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE IF NOT EXISTS common.master_data_items(id UUID PRIMARY KEY,type_id UUID NOT NULL REFERENCES common.master_data_types(id),code VARCHAR(100) NOT NULL,name VARCHAR(255) NOT NULL,value JSONB NOT NULL DEFAULT '{}',sort_order INT NOT NULL DEFAULT 0,is_active BOOLEAN NOT NULL DEFAULT TRUE,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),deleted_at TIMESTAMPTZ)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS common_master_item_unique ON common.master_data_items(type_id,code) WHERE deleted_at IS NULL`,
		`CREATE TABLE IF NOT EXISTS common.system_configs(key VARCHAR(150) PRIMARY KEY,value JSONB NOT NULL,description TEXT,updated_by UUID,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE IF NOT EXISTS common.feature_flags(key VARCHAR(150) PRIMARY KEY,enabled BOOLEAN NOT NULL DEFAULT FALSE,description TEXT,updated_by UUID,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE IF NOT EXISTS common.document_sequences(document_type VARCHAR(10) PRIMARY KEY,current_value BIGINT NOT NULL DEFAULT 0,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE IF NOT EXISTS common.data_versions(dataset VARCHAR(100) PRIMARY KEY,version VARCHAR(100) NOT NULL,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE IF NOT EXISTS common.thai_provinces(code INT PRIMARY KEY,name_th VARCHAR(150) NOT NULL,name_en VARCHAR(150) NOT NULL,region_code INT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS common.thai_districts(code INT PRIMARY KEY,province_code INT NOT NULL REFERENCES common.thai_provinces(code),name_th VARCHAR(150) NOT NULL,name_en VARCHAR(150) NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS common_thai_districts_province_idx ON common.thai_districts(province_code,code)`,
		`CREATE TABLE IF NOT EXISTS common.thai_subdistricts(code INT PRIMARY KEY,district_code INT NOT NULL REFERENCES common.thai_districts(code),province_code INT NOT NULL REFERENCES common.thai_provinces(code),name_th VARCHAR(150) NOT NULL,name_en VARCHAR(150) NOT NULL,postal_code CHAR(5) NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS common_thai_subdistricts_district_idx ON common.thai_subdistricts(district_code,code)`,
		`CREATE INDEX IF NOT EXISTS common_thai_subdistricts_province_idx ON common.thai_subdistricts(province_code,code)`,
		`CREATE INDEX IF NOT EXISTS common_thai_subdistricts_postal_idx ON common.thai_subdistricts(postal_code)`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			return err
		}
	}
	if err := seedThaiLocations(db); err != nil {
		return err
	}
	queries := map[string]string{"CUS": `SELECT COALESCE(MAX((regexp_match(customer_no,'([0-9]+)$'))[1]::bigint),0) FROM customer.customers`, "PRD": `SELECT COALESCE(MAX((regexp_match(product_no,'([0-9]+)$'))[1]::bigint),0) FROM catalog.products`, "ORD": `SELECT COALESCE(MAX((regexp_match(order_number,'([0-9]+)$'))[1]::bigint),0) FROM ordering.orders`, "PAY": `SELECT COALESCE(MAX((regexp_match(payment_number,'([0-9]+)$'))[1]::bigint),0) FROM ordering.payments`, "INV": `SELECT COALESCE(MAX((regexp_match(invoice_number,'([0-9]+)$'))[1]::bigint),0) FROM ordering.invoices`, "REF": `SELECT COALESCE(MAX((regexp_match(refund_number,'([0-9]+)$'))[1]::bigint),0) FROM ordering.refunds`}
	for kind, q := range queries {
		var current int64
		if err := db.Raw(q).Scan(&current).Error; err != nil && !strings.Contains(err.Error(), "does not exist") {
			return err
		}
		if err := db.Exec(`INSERT INTO common.document_sequences(document_type,current_value) VALUES (?,?) ON CONFLICT(document_type) DO UPDATE SET current_value=GREATEST(common.document_sequences.current_value,EXCLUDED.current_value),updated_at=NOW()`, kind, current).Error; err != nil {
			return err
		}
	}
	return nil
}

func seedThaiLocations(db *gorm.DB) error {
	const dataset = "thai-address"
	const version = "go-thaiaddress-v0.2.0"
	var current string
	db.Raw(`SELECT version FROM common.data_versions WHERE dataset=?`, dataset).Scan(&current)
	if current == version {
		return nil
	}
	return db.Transaction(func(tx *gorm.DB) error {
		provinces := make([]thaiProvince, 0, 77)
		for _, item := range thaiaddress.Provinces() {
			provinces = append(provinces, thaiProvince{
				Code: item.Code, NameTH: item.NameTH, NameEN: item.NameEN, RegionCode: int(item.Region),
			})
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "code"}},
			DoUpdates: clause.AssignmentColumns([]string{"name_th", "name_en", "region_code"}),
		}).CreateInBatches(provinces, 200).Error; err != nil {
			return err
		}
		districts := make([]thaiDistrict, 0, 928)
		for _, item := range thaiaddress.Districts() {
			districts = append(districts, thaiDistrict{
				Code: item.Code, ProvinceCode: item.ProvinceCode, NameTH: item.NameTH, NameEN: item.NameEN,
			})
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "code"}},
			DoUpdates: clause.AssignmentColumns([]string{"province_code", "name_th", "name_en"}),
		}).CreateInBatches(districts, 500).Error; err != nil {
			return err
		}
		subdistricts := make([]thaiSubdistrict, 0, 7452)
		for _, item := range thaiaddress.Subdistricts() {
			subdistricts = append(subdistricts, thaiSubdistrict{
				Code: item.Code, DistrictCode: item.DistrictCode, ProvinceCode: item.DistrictCode / 100,
				NameTH: item.NameTH, NameEN: item.NameEN, PostalCode: fmt.Sprintf("%05d", item.Postcode),
			})
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "code"}},
			DoUpdates: clause.AssignmentColumns(
				[]string{"district_code", "province_code", "name_th", "name_en", "postal_code"},
			),
		}).CreateInBatches(subdistricts, 500).Error; err != nil {
			return err
		}
		return tx.Exec(
			`INSERT INTO common.data_versions(dataset,version,updated_at) VALUES (?,?,NOW()) ON CONFLICT(dataset) DO UPDATE SET version=EXCLUDED.version,updated_at=NOW()`,
			dataset, version,
		).Error
	})
}
func server(db *gorm.DB, auth string) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.Validator = validation{validator.New()}
	e.Use(middleware.Recover(), middleware.RequestID(), middleware.CORS())
	e.GET("/api/v1/health", func(c echo.Context) error {
		sqlDB, err := db.DB()
		if err != nil || sqlDB.PingContext(c.Request().Context()) != nil {
			return c.JSON(503, map[string]string{"status": "unhealthy"})
		}
		return c.JSON(200, map[string]string{"status": "ok"})
	})
	api := e.Group("/api/v1", authenticate(auth))
	api.GET("/master-data/types", func(c echo.Context) error {
		var out []masterType
		if err := db.Order("code").Find(&out).Error; err != nil {
			return err
		}
		return c.JSON(200, out)
	}, require("common.read"))
	api.POST("/master-data/types", createType(db), require("common.write"))
	api.GET("/master-data/:type", listItems(db), require("common.read"))
	api.POST("/master-data/:type", createItem(db), require("common.write"))
	api.PATCH("/master-data/:type/:id", updateItem(db), require("common.write"))
	api.DELETE("/master-data/:type/:id", deleteItem(db), require("common.write"))
	api.GET("/system-configs", func(c echo.Context) error {
		var out []setting
		if err := db.Order("key").Find(&out).Error; err != nil {
			return err
		}
		return c.JSON(200, out)
	}, require("common.read"))
	api.PATCH("/system-configs/:key", updateSetting(db), require("common.write"))
	api.GET("/feature-flags", func(c echo.Context) error {
		var out []flag
		if err := db.Order("key").Find(&out).Error; err != nil {
			return err
		}
		return c.JSON(200, out)
	}, require("common.read"))
	api.PATCH("/feature-flags/:key", updateFlag(db), require("common.write"))
	api.GET("/locations/provinces", listThaiProvinces(db), require("common.read"))
	api.GET("/locations/districts", listThaiDistricts(db), require("common.read"))
	api.GET("/locations/subdistricts", listThaiSubdistricts(db), require("common.read"))
	api.GET("/locations/search", searchThaiLocations(db), require("common.read"))
	api.POST("/document-numbers/next", nextNumber(db), requireService("document-numbers.issue"))
	return e
}
func authenticate(auth string) echo.MiddlewareFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := strings.TrimSpace(c.Request().Header.Get("Authorization"))
			if !strings.HasPrefix(h, "Bearer ") {
				return echo.NewHTTPError(401, "Bearer token is required")
			}
			body, _ := json.Marshal(map[string]string{"token": strings.TrimPrefix(h, "Bearer ")})
			req, _ := http.NewRequestWithContext(c.Request().Context(), "POST", auth+"/api/v1/auth/validate-token", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			res, err := client.Do(req)
			if err != nil || res.StatusCode != 200 {
				if res != nil {
					res.Body.Close()
				}
				return echo.NewHTTPError(401, "Invalid token")
			}
			defer res.Body.Close()
			var u user
			if json.NewDecoder(res.Body).Decode(&u) != nil {
				return echo.NewHTTPError(401, "Invalid token")
			}
			c.Set("user", u)
			return next(c)
		}
	}
}
func require(p string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if !c.Get("user").(user).has(p) {
				return echo.NewHTTPError(403, "Insufficient permission")
			}
			return next(c)
		}
	}
}
func requireService(p string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			u := c.Get("user").(user)
			if u.Type != "service" || !u.has(p) {
				return echo.NewHTTPError(403, "Service permission required")
			}
			return next(c)
		}
	}
}
func getType(db *gorm.DB, code string) (masterType, error) {
	var out masterType
	err := db.Where("code=? AND is_active=TRUE", strings.ToUpper(code)).First(&out).Error
	return out, err
}
func createType(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		var in typeInput
		if c.Bind(&in) != nil || c.Validate(&in) != nil {
			return echo.NewHTTPError(400, "Invalid body")
		}
		out := masterType{ID: uuid.New(), Code: in.Code, Name: in.Name, Description: in.Description, IsActive: true}
		if err := db.Create(&out).Error; err != nil {
			return dbError(err)
		}
		return c.JSON(201, out)
	}
}
func listItems(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		t, err := getType(db, c.Param("type"))
		if err != nil {
			return echo.NewHTTPError(404, "Master data type not found")
		}
		var out []masterItem
		q := db.Where("type_id=?", t.ID)
		if c.QueryParam("active") == "true" {
			q = q.Where("is_active=TRUE")
		}
		if err = q.Order("sort_order,code").Find(&out).Error; err != nil {
			return err
		}
		return c.JSON(200, out)
	}
}
func createItem(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		t, err := getType(db, c.Param("type"))
		if err != nil {
			return echo.NewHTTPError(404, "Master data type not found")
		}
		var in itemInput
		if c.Bind(&in) != nil || c.Validate(&in) != nil {
			return echo.NewHTTPError(400, "Invalid body")
		}
		active := true
		if in.IsActive != nil {
			active = *in.IsActive
		}
		if in.Value == nil {
			in.Value = map[string]any{}
		}
		out := masterItem{ID: uuid.New(), TypeID: t.ID, Code: in.Code, Name: in.Name, Value: in.Value, SortOrder: in.SortOrder, IsActive: active}
		if err := db.Create(&out).Error; err != nil {
			return dbError(err)
		}
		return c.JSON(201, out)
	}
}
func updateItem(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		t, err := getType(db, c.Param("type"))
		if err != nil {
			return echo.NewHTTPError(404, "Master data type not found")
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(400, "Invalid id")
		}
		var in itemInput
		if c.Bind(&in) != nil || c.Validate(&in) != nil {
			return echo.NewHTTPError(400, "Invalid body")
		}
		updates := map[string]any{"code": in.Code, "name": in.Name, "value": in.Value, "sort_order": in.SortOrder, "updated_at": time.Now().UTC()}
		if in.IsActive != nil {
			updates["is_active"] = *in.IsActive
		}
		r := db.Model(&masterItem{}).Where("id=? AND type_id=?", id, t.ID).Updates(updates)
		if r.Error != nil {
			return dbError(r.Error)
		}
		if r.RowsAffected == 0 {
			return echo.NewHTTPError(404, "Item not found")
		}
		var out masterItem
		db.First(&out, "id=?", id)
		return c.JSON(200, out)
	}
}
func deleteItem(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		t, err := getType(db, c.Param("type"))
		if err != nil {
			return echo.NewHTTPError(404, "Master data type not found")
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			return echo.NewHTTPError(400, "Invalid id")
		}
		r := db.Where("id=? AND type_id=?", id, t.ID).Delete(&masterItem{})
		if r.Error != nil {
			return r.Error
		}
		if r.RowsAffected == 0 {
			return echo.NewHTTPError(404, "Item not found")
		}
		return c.NoContent(204)
	}
}
func updateSetting(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		key := c.Param("key")
		if secretKey(key) {
			return echo.NewHTTPError(400, "Secrets are not allowed")
		}
		var in valueInput
		if c.Bind(&in) != nil || c.Validate(&in) != nil {
			return echo.NewHTTPError(400, "Invalid body")
		}
		u := c.Get("user").(user)
		actor, _ := uuid.Parse(u.Sub)
		out := setting{Key: key, Value: in.Value, Description: in.Description, UpdatedBy: &actor, UpdatedAt: time.Now().UTC()}
		if err := db.Save(&out).Error; err != nil {
			return err
		}
		return c.JSON(200, out)
	}
}
func updateFlag(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		var in flagInput
		if c.Bind(&in) != nil {
			return echo.NewHTTPError(400, "Invalid body")
		}
		u := c.Get("user").(user)
		actor, _ := uuid.Parse(u.Sub)
		out := flag{Key: c.Param("key"), Enabled: in.Enabled, Description: in.Description, UpdatedBy: &actor, UpdatedAt: time.Now().UTC()}
		if err := db.Save(&out).Error; err != nil {
			return err
		}
		return c.JSON(200, out)
	}
}

func listThaiProvinces(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		var out []thaiProvince
		query := db.Order("name_th")
		if q := strings.TrimSpace(c.QueryParam("q")); q != "" {
			like := "%" + q + "%"
			query = query.Where("name_th ILIKE ? OR name_en ILIKE ?", like, like)
		}
		if err := query.Find(&out).Error; err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	}
}

func listThaiDistricts(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		provinceCode, err := requiredPositiveInt(c.QueryParam("provinceCode"), "provinceCode")
		if err != nil {
			return err
		}
		var out []thaiDistrict
		query := db.Where("province_code=?", provinceCode).Order("name_th")
		if q := strings.TrimSpace(c.QueryParam("q")); q != "" {
			like := "%" + q + "%"
			query = query.Where("name_th ILIKE ? OR name_en ILIKE ?", like, like)
		}
		if err := query.Find(&out).Error; err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	}
}

func listThaiSubdistricts(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		districtCode, err := requiredPositiveInt(c.QueryParam("districtCode"), "districtCode")
		if err != nil {
			return err
		}
		var out []thaiSubdistrict
		query := db.Where("district_code=?", districtCode).Order("name_th")
		if q := strings.TrimSpace(c.QueryParam("q")); q != "" {
			like := "%" + q + "%"
			query = query.Where("name_th ILIKE ? OR name_en ILIKE ?", like, like)
		}
		if postalCode := strings.TrimSpace(c.QueryParam("postalCode")); postalCode != "" {
			query = query.Where("postal_code=?", postalCode)
		}
		if err := query.Find(&out).Error; err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	}
}

func searchThaiLocations(db *gorm.DB) echo.HandlerFunc {
	return func(c echo.Context) error {
		q := strings.TrimSpace(c.QueryParam("q"))
		postalCode := strings.TrimSpace(c.QueryParam("postalCode"))
		if q == "" && postalCode == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "q or postalCode is required")
		}
		limit := queryLimit(c.QueryParam("limit"), 20, 100)
		query := db.Table("common.thai_subdistricts s").
			Select(`p.code province_code,p.name_th province_name_th,p.name_en province_name_en,
				d.code district_code,d.name_th district_name_th,d.name_en district_name_en,
				s.code subdistrict_code,s.name_th subdistrict_name_th,s.name_en subdistrict_name_en,
				s.postal_code postal_code`).
			Joins("JOIN common.thai_districts d ON d.code=s.district_code").
			Joins("JOIN common.thai_provinces p ON p.code=s.province_code")
		if q != "" {
			like := "%" + q + "%"
			query = query.Where(
				`s.name_th ILIKE ? OR s.name_en ILIKE ? OR d.name_th ILIKE ? OR d.name_en ILIKE ? OR p.name_th ILIKE ? OR p.name_en ILIKE ?`,
				like, like, like, like, like, like,
			)
		}
		if postalCode != "" {
			query = query.Where("s.postal_code=?", postalCode)
		}
		var out []thaiLocation
		if err := query.Order("p.name_th,d.name_th,s.name_th").Limit(limit).Scan(&out).Error; err != nil {
			return err
		}
		return c.JSON(http.StatusOK, out)
	}
}

func requiredPositiveInt(value string, field string) (int, error) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number <= 0 {
		return 0, echo.NewHTTPError(http.StatusBadRequest, field+" is required")
	}
	return number, nil
}

func queryLimit(value string, fallback int, maximum int) int {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number <= 0 {
		return fallback
	}
	if number > maximum {
		return maximum
	}
	return number
}

func nextNumber(db *gorm.DB) echo.HandlerFunc {
	loc, _ := time.LoadLocation("Asia/Bangkok")
	return func(c echo.Context) error {
		var in numberInput
		if c.Bind(&in) != nil || c.Validate(&in) != nil {
			return echo.NewHTTPError(400, "Invalid document type")
		}
		var next int64
		if err := db.Raw(`UPDATE common.document_sequences SET current_value=current_value+1,updated_at=NOW() WHERE document_type=? RETURNING current_value`, in.DocumentType).Scan(&next).Error; err != nil {
			return err
		}
		if next == 0 {
			return echo.NewHTTPError(404, "Sequence not found")
		}
		return c.JSON(200, map[string]any{"documentType": in.DocumentType, "sequence": next, "number": formatNumber(in.DocumentType, next, time.Now().In(loc))})
	}
}
func formatNumber(prefix string, sequence int64, at time.Time) string {
	return fmt.Sprintf("%s-%s-%06d", prefix, at.Format("20060102"), sequence)
}
func secretKey(v string) bool {
	v = strings.ToLower(v)
	for _, w := range []string{"password", "secret", "token", "private_key", "api_key"} {
		if strings.Contains(v, w) {
			return true
		}
	}
	return false
}
func dbError(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "duplicate") {
		return echo.NewHTTPError(409, "Resource already exists")
	}
	return err
}
