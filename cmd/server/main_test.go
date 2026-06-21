package main

import (
	"testing"
	"time"

	thaiaddress "github.com/ultramcu/go-thaiaddress"
)

func TestFormatNumber(t *testing.T) {
	if got := formatNumber("ORD", 42, time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)); got != "ORD-20260621-000042" {
		t.Fatal(got)
	}
}
func TestSecretKey(t *testing.T) {
	if !secretKey("smtp.password") || secretKey("company.timezone") {
		t.Fatal("secret validation")
	}
}

func TestQueryLimit(t *testing.T) {
	if queryLimit("", 20, 100) != 20 || queryLimit("250", 20, 100) != 100 || queryLimit("15", 20, 100) != 15 {
		t.Fatal("query limit")
	}
}

func TestThaiAddressDataset(t *testing.T) {
	if len(thaiaddress.Provinces()) != 77 || len(thaiaddress.Districts()) < 900 || len(thaiaddress.Subdistricts()) < 7400 {
		t.Fatal("thai address dataset is incomplete")
	}
}
