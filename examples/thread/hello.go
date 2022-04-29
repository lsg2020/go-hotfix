package main

import (
	"fmt"
	"time"

	go_hotfix "github.com/lsg2020/go-hotfix"
	"github.com/lsg2020/go-hotfix/examples/data"
)

func Hotfix() {
	main()
}

func main() {
	test := func(workId int) {
		p1 := data.DataType{Str: "p1", A: 1, B: 1}
		p2 := data.DataType{Str: "p2", A: 2, B: 2}

		r := data.TestAdd(p1, p2)
		r.TestHotfix()
	}

	for i := 0; i < 10; i++ {
		workId := i
		go func() {
			for {
				for step := 0; step < 10000; step++ {
					test(workId)
				}
				// time.Sleep(time.Millisecond)
			}
		}()
	}

	hotFunctions := []string{
		"github.com/lsg2020/go-hotfix/examples/data.TestAdd",
		"github.com/lsg2020/go-hotfix/examples/data.(*DataType).TestHotfix",
	}

	time.Sleep(time.Second)
	fmt.Println("--------------------------- hello_v1.so")
	err := go_hotfix.Hotfix("hello_v1.so", hotFunctions, false)
	if err != nil {
		panic(err)
	}

	time.Sleep(time.Second)
}
