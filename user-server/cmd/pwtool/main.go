package main

import (
	"fmt"

	"hivemtk-user/internal/pkg/utils/bcrypt"
)

func main() {
	h, err := bcrypt.HashPassword("Seed@123456")
	if err != nil {
		panic(err)
	}
	fmt.Println(h)
}
