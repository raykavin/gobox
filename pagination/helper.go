package pagination

import "reflect"

// derefVal unwraps pointer values so filters always store the
// underlying value. Nil pointers become nil.
func derefVal(v any) any {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		return rv.Elem().Interface()
	}
	return v
}
