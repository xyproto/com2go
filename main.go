package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"os"
)

const disasmCommand = "/usr/bin/ndisasm -a -b 16 %s | cut -b29-"

var seen []string

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

// TODO: Add all of them
var registers = []string{"al", "ah", "ax", "bl", "bh", "bx", "cl", "ch", "cx", "dl", "dh", "dx", "es", "cs", "di"}

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
			return "0"
		}
		fmt.Println("HAS SEEN " + r + " BEFORE!")
		return r + ".Get()"
	}
	fmt.Println("GETVAL", s)
	return s
}

func interpret(s string) string {
	if isValue(s) {
		return s
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		registerOrMemory := s[1:len(s)-1]
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
	fmt.Println("INTERPRET", s)
	return s
}

func main() {
	fn := "example.com"
	if len(os.Args) > 1 {
		fn = os.Args[1]
	}
	_, err := os.Stat(fn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Could not find " + fn)
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
	"encoding/binary"
	"time"
)

var frameUpdate = 100 * time.Millisecond

type PixelBuffer []uint8 // 320x200

type Register struct {
	l uint8
	h uint8
}

func (r *Register) SetL(l uint8) {
	r.l = l
}

func (r *Register) SetH(h uint8) {
	r.h = h
}

func (r *Register) Set(w uint16) {
	bs := make([]uint8, 2)
	binary.LittleEndian.PutUint16(bs, w)
	r.l, r.h = bs[0], bs[1]
}

func (r *Register) SetR(e *Register) {
	r.l = e.l
	r.h = e.h
}

func (r *Register) Get() uint16 {
	return uint16(r.l) + uint16(r.h << 1)
}

func interrupt(n int) {
	fmt.Printf("CALLING INTERRUPT %d\n", n)
}

func draw(pixelbuffer PixelBuffer) {
	fmt.Println("UPDATING SCREEN")
}

func main() {
	var mem [640*1024]uint8
	go func() {
		// TODO: Also read from keyboard
		// TODO: Support a palette as well
		draw(PixelBuffer(mem[0xa000:0xa000+(320*200)]))
		time.Sleep(frameUpdate)
	}()
`
	for _, line := range mapS(lines, noCommentStripped) {
		if strings.HasPrefix(line, "mov") {
			fields := strings.Split(line[3:], ",")
			if len(fields) > 2 {
				fmt.Fprintln(os.Stderr, "Too many commas: " + line)
				os.Exit(1)
			}
			registerOrMemory := strings.TrimSpace(fields[0])
			valueOrRegisterOrMemory := strings.TrimSpace(fields[1])

			if isRegister(registerOrMemory) {
				register := registerOrMemory
				r := shortName(register) // al, ah, ax -> a
				if !has(seen, r) {
					gocode += "\t" + r + " := &Register{}" + "\n"
					seen = append(seen, r)
				}
				if isRegister(valueOrRegisterOrMemory) {
					gocode += "\t" + r + ".SetR(" + interpret(valueOrRegisterOrMemory) + ")" + " // " + line + "\n"
				} else {
					if strings.HasSuffix(register, "h") {
						gocode += "\t" + r + ".SetH(" + interpret(valueOrRegisterOrMemory) + ") // " + line + "\n"
					} else if strings.HasSuffix(register, "l") {
						gocode += "\t" + r + ".SetL(" + interpret(valueOrRegisterOrMemory) + ") // " + line + "\n"
					} else {
						gocode += "\t" + r + ".Set(" + interpret(valueOrRegisterOrMemory) + ") // " + line + "\n"
					}
				}
			} else {
				gocode += "\tmem.Set(" + registerOrMemory[1:len(registerOrMemory)-1] + ", " + interpret(valueOrRegisterOrMemory) + ")" + " // " + line + "\n"
			}
		} else if strings.HasPrefix(line, "int") {
			fields := strings.Split(line, " ")
			if len(fields) < 2 {
				fmt.Fprintln(os.Stderr, "Too few arguments to int: " + line)
				os.Exit(1)
			}
			gocode += "\tinterrupt(" + fields[1] + ") // " + line + "\n"
		} else {
			gocode += "\t// " + line + "\n"
		}
	}
	gocode += "}\n"
	fmt.Println(gocode)
}
