package main

import (
	"encoding/json"
	"fmt"
)

type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Token string `json:"-"`
}

func main() {
	u := User{ID: 7, Name: "alice", Token: "secret"}
	b, err := json.Marshal(u)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))

	var back User
	if err := json.Unmarshal(b, &back); err != nil {
		panic(err)
	}
	fmt.Printf("%+v\n", back)
}
