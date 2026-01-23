package sample

import "fmt"

// Greet returns a greeting for the given name.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

// Farewell returns a farewell for the given name.
func Farewell(name string) string {
	return fmt.Sprintf("Goodbye, %s!", name)
}

type Person struct {
	Name string
	Age  int
}

func NewPerson(name string, age int) Person {
	return Person{Name: name, Age: age}
}

func (p Person) SayHello() string {
	return Greet(p.Name)
}
