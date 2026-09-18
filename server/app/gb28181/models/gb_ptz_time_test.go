package models

import (
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

func TestPTZSQLServerWallTimeScan(t *testing.T) {
	previous := time.Local
	time.Local = time.FixedZone("PTZ-test-CST", 8*60*60)
	t.Cleanup(func() { time.Local = previous })
	wire := time.Date(2026, 9, 8, 16, 8, 47, 402000000, time.UTC)
	want := time.Date(2026, 9, 8, 16, 8, 47, 402000000, time.Local)
	tx := &gorm.DB{Config: &gorm.Config{Dialector: sqlserver.New(sqlserver.Config{})}}
	for _, model := range []any{&GbPTZOperation{}, &GbPTZOperationAttempt{}} {
		v := reflect.ValueOf(model).Elem()
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.Type() == reflect.TypeOf(wire) {
				f.Set(reflect.ValueOf(wire))
			}
			if f.Type() == reflect.TypeOf(&wire) {
				copy := wire
				f.Set(reflect.ValueOf(&copy))
			}
		}
		hook, ok := model.(interface{ AfterFind(*gorm.DB) error })
		if !ok {
			t.Fatalf("%T has no SQL Server wall-time scan boundary", model)
		}
		if err := hook.AfterFind(tx); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			if f.Type() == reflect.TypeOf(&wire) {
				f = f.Elem()
			}
			if f.IsValid() && f.Type() == reflect.TypeOf(wire) && !f.Interface().(time.Time).Equal(want) {
				t.Errorf("%T.%s changed actual instant: %v want %v", model, v.Type().Field(i).Name, f.Interface(), want)
			}
		}
	}
}
