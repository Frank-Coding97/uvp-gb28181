package models

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm/schema"
)

func TestUserMustChangePasswordFieldContract(t *testing.T) {
	parsed, err := schema.Parse(&User{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)

	field, ok := parsed.FieldsByDBName["must_change_password"]
	require.True(t, ok, "User 必须映射 must_change_password 列")
	require.Equal(t, "mustChangePassword", field.StructField.Tag.Get("json"))
	require.True(t, field.NotNull, "must_change_password 必须 NOT NULL")
	require.Equal(t, "false", field.DefaultValue, "旧库迁移与普通用户默认值必须为 false")
}
