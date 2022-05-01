package main

import (
	"fmt"

	go_hotfix "github.com/lsg2020/go-hotfix"
	"github.com/lsg2020/go-hotfix/examples/data"
)

var HotfixVersion = "1"

func Hotfix() {
	main()
}

func testPrint(p data.DataType) {
	fmt.Printf("main print %#v \n", p)
}

func main() {
	test := func() {
		p1 := data.DataType{Str: "p1", A: 1, B: 1}
		p2 := data.DataType{Str: "p2", A: 2, B: 2}

		r := data.TestAdd(p1, p2)
		testPrint(*r)

		testPrint(p1)
		p1.TestHotfix()
		testPrint(p1)
	}
	test()

	hotFunctions := []string{
		"github.com/lsg2020/go-hotfix/examples/data.testPrivateFunc",
		"github.com/lsg2020/go-hotfix/examples/data.(*DataType).test",
	}

	_, err := go_hotfix.Hotfix("hello_v1.so", hotFunctions, false)
	if err != nil {
		panic(err)
	}

	fmt.Println("--------------------------- hello_v1.so")
	test()

	_, err = go_hotfix.Hotfix("hello_v2.so", hotFunctions, false)
	if err != nil {
		panic(err)
	}

	fmt.Println("--------------------------- hello_v2.so")
	test()

}
