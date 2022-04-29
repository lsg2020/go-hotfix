package data

import (
	"strconv"
)

type DataType struct {
	Str string
	A   int
	B   int
}

var AddValue1 = "0"

func (d *DataType) TestHotfix() {
	v, _ := strconv.ParseInt(AddValue1, 10, 32)
	d.A += int(v)
	d.B += int(v)
}

func TestAdd(p1 DataType, p2 DataType) *DataType {
	v, _ := strconv.ParseInt(AddValue1, 10, 32)
	return &DataType{
		Str: p1.Str,
		A:   p1.A + p2.A + int(v),
		B:   p1.B + p2.B + int(v),
	}
}
