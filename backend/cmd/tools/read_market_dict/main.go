package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"cloud.google.com/go/firestore"
)

func pretty(v interface{}) string {
	b, _ := json.MarshalIndent(v, "", "  ")
	return string(b)
}

func main() {
	ctx := context.Background()
	project := os.Getenv("GCP_PROJECT_ID")
	if project == "" { project = "etigram" }
	job := "MHQjrZ2a1bB2NIbILYAh"
	c, err := firestore.NewClient(ctx, project)
	if err != nil { panic(err) }
	defer c.Close()
	doc, err := c.Collection("horizon_policy_check_results").Doc(job).Get(ctx)
	if err != nil { panic(err) }
	m := doc.Data()
	fmt.Println(pretty(m["violations"]))
}
