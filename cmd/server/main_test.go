package main

import (
	"testing"
	"time"
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
