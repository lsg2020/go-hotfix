package data

import (
	"fmt"
)

type DataType struct {
	Str string
	A   int
	B   int
}

type privateStruct struct {
	A int
}

var AddValue = "0"
var addValue = 100

func testPrivateFunc(d *DataType, dd *privateStruct) {
	d.A++
	dd.A++
	fmt.Println("in testPrivateFunc v2", dd)
}

func (d *DataType) test() {
	fmt.Println("in func (d *DataType) test()")
	d.A += 10
}

func (d *DataType) TestHotfix() {
	testPrivateFunc(d, &privateStruct{A: 1234})
	d.test()

	d.A += addValue
	d.B += addValue
}

func TestAdd(p1 DataType, p2 DataType) *DataType {
	return &DataType{
		Str: p1.Str,
		A:   p1.A + p2.A + addValue,
		B:   p1.B + p2.B + addValue,
	}
}
