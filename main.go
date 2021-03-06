package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const disasmCommand = "/usr/bin/ndisasm -a -b 16 %s | cut -b29-"

// Encountered registers, filled with the ones declared at the top
var seen = []string{"a", "b", "c", "d", "es", "cs", "di", "ds"}

func shellCommand(command string) (string, error) {
	var (
		args  []string
		shell = "/bin/sh"
	)
	args = []string{"-c", command}
	//fmt.Printf("Running: %s %s\n", shell, strings.Join(args, " "))
	cmd := exec.Command(shell, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	err := cmd.Run()
	return buf.String(), err
}

func disasm(filename string) (string, error) {
	return shellCommand(fmt.Sprintf(disasmCommand, filename))
}

// Map a string function to a string slice, and skip empty strings
func mapS(sl []string, f func(string) string) []string {
	var newsl []string
	for _, e := range sl {
		s := f(e)
		if len(s) > 0 {
			newsl = append(newsl, s)
		}
	}
	return newsl
}

// Trip away leading and trailing space, and remove any comments after ";"
func noCommentStripped(s string) string {
	if strings.Contains(s, ";") {
		fields := strings.SplitN(s, ";", 2)
		return strings.TrimSpace(fields[0])
	}
	return strings.TrimSpace(s)
}

func has(sl []string, s string) bool {
	for _, e := range sl {
		if e == s {
			return true
		}
	}
	return false
}

func shortName(register string) string {
	switch register {
	case "al", "ah", "ax":
		return "a"
	case "bl", "bh", "bx":
		return "b"
	case "cl", "ch", "cx":
		return "c"
	case "dl", "dh", "dx":
		return "d"
	}
	return register
}

// Return the Registers method needed to call to get the Register
// struct in question
func rFuncName(register string) string {
	return strings.ToUpper(shortName(register)) + "()"
}

var registers = []string{"al", "ah", "ax", "bl", "bh", "bx", "cl", "ch", "cx", "dl", "dh", "dx", "si", "di", "sp", "bp", "ip", "cs", "es", "ds", "fs", "gs", "ss"}

func isRegister(register string) bool {
	return has(registers, register)
}

func isValue(s string) bool {
	for _, letter := range s {
		switch letter {
		case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'x':
			return true
		}
	}
	return false
}

func getVal(s string) string {
	if isValue(s) {
		return s
	}
	if isRegister(s) {
		r := shortName(s)
		if !has(seen, r) {
			// TODO: Log a warning to stderr?
			return "0"
		}
		return "reg." + rFuncName(s) + ".Get()"
	}
	return s
}

func interpret(s string) string {
	if isValue(s) {
		return s
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		registerOrMemory := s[1 : len(s)-1]
		if isRegister(registerOrMemory) {
			retval := "mem[" + getVal(registerOrMemory) + "]"
			if !has(seen, registerOrMemory) {
				seen = append(seen, registerOrMemory)
			}
			return retval
		} else {
			// !?!
			return "mem[mem[" + registerOrMemory + "]]"
		}
	}
	//fmt.Println("INTERPRET", s)
	return s
}

func main() {
	fn := "life.com"
	if len(os.Args) > 1 {
		fn = os.Args[1]
	}
	_, err := os.Stat(fn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not find "+fn)
		os.Exit(1)
	}
	data, err := disasm(fn)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lines := strings.Split(data, "\n")
	gocode := `package main

import (
	"fmt"
	"time"
	dos "github.com/xyproto/interrupts"
)

var (
	reg dos.Registers
	mem dos.Memory
	stack dos.Stack
	flags dos.Flags
	state = &dos.State{&reg, &mem, &stack, &flags}
)

func main() {
	dos.Init()
	go dos.Loop(&mem)
`
	for _, line := range mapS(lines, noCommentStripped) {
		if strings.HasPrefix(line, "mov") {
			fields := strings.Split(line[3:], ",")
			if len(fields) > 2 {
				fmt.Fprintln(os.Stderr, "Too many commas: "+line)
				os.Exit(1)
			}
			registerOrMemory := strings.TrimSpace(fields[0])
			valueOrRegisterOrMemory := strings.TrimSpace(fields[1])

			if isRegister(registerOrMemory) {
				register := registerOrMemory
				r := shortName(register) // al, ah, ax -> a
				rfn := rFuncName(register)
				if !has(seen, r) {
					panic("TO IMPLEMENT")
					gocode += "\t" + r + " := &dos.Register{}" + "\n"
					seen = append(seen, r)
				}
				if isRegister(valueOrRegisterOrMemory) {
					gocode += "\treg." + rfn + ".SetR(" + interpret(valueOrRegisterOrMemory) + ")" + " // " + line + "\n"
				} else {
					if strings.HasSuffix(register, "h") {
						gocode += "\treg." + rfn + ".SetH(" + interpret(valueOrRegisterOrMemory) + ") // " + line + "\n"
					} else if strings.HasSuffix(register, "l") {
						gocode += "\treg." + rfn + ".SetL(" + interpret(valueOrRegisterOrMemory) + ") // " + line + "\n"
					} else {
						gocode += "\treg." + rfn + ".Set(" + interpret(valueOrRegisterOrMemory) + ") // " + line + "\n"
					}
				}
			} else {
				gocode += "\tmem.Set(" + registerOrMemory[1:len(registerOrMemory)-1] + ", " + interpret(valueOrRegisterOrMemory) + ")" + " // " + line + "\n"
			}
		} else if strings.HasPrefix(line, "int") {
			fields := strings.Split(line, " ")
			if len(fields) < 2 {
				fmt.Fprintln(os.Stderr, "Too few arguments to int: "+line)
				os.Exit(1)
			}
			gocode += "\tdos.Interrupt(" + fields[1] + ", state) // " + line + "\n"
		} else if strings.HasPrefix(line, "push") {
			fields := strings.Split(line, " ")
			if len(fields) < 2 {
				fmt.Fprintln(os.Stderr, "Too few arguments to push: "+line)
				os.Exit(1)
			}
			valueOrRegisterOrMemory := fields[1]
			if isRegister(valueOrRegisterOrMemory) {
				rfn := rFuncName(valueOrRegisterOrMemory)
				gocode += "\tstack = append(stack, reg." + rfn + ".Get()) // " + line + "\n"
			} else if isValue(valueOrRegisterOrMemory) {
				panic("PUSHING VALUES DIRECTLY TO THE STACK IS NOT IMPLEMENTED YET: " + line)
			} else {
				panic("PUSHING MEMORY LOCATIONS TO THE STACK IS NOT IMPLEMENTED YET: " + line)
			}
		} else if strings.HasPrefix(line, "pop") {
			fields := strings.Split(line, " ")
			if len(fields) < 2 {
				fmt.Fprintln(os.Stderr, "Too few arguments to pop: "+line)
				os.Exit(1)
			}
			valueOrRegisterOrMemory := fields[1]
			if isRegister(valueOrRegisterOrMemory) {
				rfn := rFuncName(valueOrRegisterOrMemory)
				gocode += "\treg." + rfn + ".Set(stack[len(stack)-1]); "
				gocode += "stack = stack[:len(stack)-1] // " + line + "\n"
			} else if isValue(valueOrRegisterOrMemory) {
				panic("POPPING TO A VALUE IS NOT POSSIBLE: " + line)
			} else {
				panic("POPPING TO A MEMORY LOCATION IS NOT IMPLEMENTED: " + line)
			}
		} else {
			gocode += "\t// " + line + "\n"
		}
	}
	gocode += "\tdos.Quit()\n"
	gocode += "}\n"
	fmt.Println(gocode)
}
