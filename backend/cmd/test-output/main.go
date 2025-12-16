package main

import (
	"fmt"
	"os"
	"time"
)

func main() {
	f, err := os.Create("internal_log.txt")
	if err != nil {
		panic(err)
	}
	defer f.Close()

	msg := fmt.Sprintf("Log from inside Go program at %s\n", time.Now().Format(time.RFC3339))
	fmt.Println(msg)
	f.WriteString(msg)
}



