package insert

import (
	"errors"
	"reflect"
	"strings"
)

var errInvalidEntity = errors.New("invalid entity")

// InsertStmt 作业里面我们这个只是生成 SQL，所以在处理 sql.NullString 之类的接口
// 只需要判断有没有实现 driver.Valuer 就可以了
func InsertStmt(entity interface{}) (string, []interface{}, error) {

	if entity == nil {
		return "", nil, errInvalidEntity
	}
	val := reflect.ValueOf(entity)
	typ := val.Type()
	if typ.Kind() == reflect.Ptr {
		val = val.Elem()
		typ = val.Type()
	}
	if typ.Kind() == reflect.Ptr {
		return "", nil, errInvalidEntity
	}
	if typ.Kind() != reflect.Struct {
		return "", nil, errInvalidEntity
	}
	if val.NumField() == 0 {
		return "", nil, errInvalidEntity
	}
	fields := make([]string, 0, typ.NumField())
	args := make([]interface{}, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.PkgPath != "" {
			continue
		}
		fields = append(fields, field.Name)
		args = append(args, val.Field(i).Interface())
	}

	values := make([]string, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		values = append(values, "?")
	}
	sqlValues := strings.Join(values, ",")
	sql := "INSERT INTO `" + typ.Name() + "`(`" + strings.Join(fields, "`,`") + "`) VALUES(" + sqlValues + ");"
	//return "INSERT INTO `" + typ.Name() + "`(" + strings.Join(fields, ",") + ") VALUES(" + strings.Join(strings.Repeat("?", len(fields))}, ",") + ");", args, nil
	return sql, args, nil
}
