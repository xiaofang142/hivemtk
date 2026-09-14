package pagination

import "reflect"

// sliceValue 解引用指针，返回底层切片值（GORM Find 要求 Dest 为切片指针）
func sliceValue(dest any) reflect.Value {
	v := reflect.ValueOf(dest)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	return v
}

func sliceLen(dest any) int {
	v := sliceValue(dest)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return 0
	}
	return v.Len()
}

func sliceTruncate(dest any, n int) {
	v := sliceValue(dest)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return
	}
	if v.Len() <= n {
		return
	}

	truncated := v.Slice(0, n)

	if v.CanSet() {
		v.Set(truncated)
	} else if v.CanAddr() {

		ptr := v.Addr()
		if ptr.CanSet() {
			ptr.Elem().Set(truncated)
		}
	}
}

func sliceAt(dest any, i int) any {
	v := sliceValue(dest)
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return nil
	}
	if i < 0 || i >= v.Len() {
		return nil
	}
	return v.Index(i).Interface()
}
