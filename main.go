package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func usageAndExit() {
	fmt.Fprintf(os.Stderr, "Usage: %s <source.bf> [out_executable]\n", filepath.Base(os.Args[0]))
	os.Exit(1)
}

func sanitizeLabel(n int) string {
	return fmt.Sprintf(".Lloop_%d", n)
}

func main() {
	if len(os.Args) < 2 {
		usageAndExit()
	}

	srcPath := os.Args[1]
	outExe := "bf_program"
	if len(os.Args) >= 3 {
		outExe = os.Args[2]
	}

	src, err := ioutil.ReadFile(srcPath)
	if err != nil {
		log.Fatalf("read file: %v", err)
	}

	// Prepare to generate assembly
	var asm bytes.Buffer

	// Header: text, intel syntax, and main symbol
	asm.WriteString("\t.intel_syntax noprefix\n")
	asm.WriteString("\t.section .text\n")
	asm.WriteString("\t.global main\n")
	asm.WriteString("\t.type main, @function\n")
	asm.WriteString("main:\n")
	// minimal prologue
	asm.WriteString("\tpush rbp\n")
	asm.WriteString("\tmov rbp, rsp\n")

	// Initialize pointer register r12 to tape base (rip-relative)
	asm.WriteString("\tlea r12, [rip + tape]\n") // r12 will be our data pointer

	// We'll implement brainfuck commands mapping to asm.
	// For loops we will create unique labels using a stack.
	loopStack := []int{}
	loopCounter := 0

	// iterate over source bytes
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case '>':
			// increment pointer
			asm.WriteString("\tadd r12, 1\n")
		case '<':
			asm.WriteString("\tsub r12, 1\n")
		case '+':
			asm.WriteString("\tinc BYTE PTR [r12]\n")
		case '-':
			asm.WriteString("\tdec BYTE PTR [r12]\n")
		case '.':
			// call putchar with the byte at [r12]
			// syscall ABI: first arg in edi
			// move zero-extended byte into edi then call putchar
			asm.WriteString("\tmovzx edi, BYTE PTR [r12]\n")
			asm.WriteString("\tcall putchar\n")
		case ',':
			// call getchar, result in eax; store al into [r12]
			asm.WriteString("\tcall getchar\n")
			asm.WriteString("\tmov BYTE PTR [r12], al\n")
		case '[':
			// create two labels: loop_begin_X and loop_end_X
			id := loopCounter
			loopCounter++
			loopStack = append(loopStack, id)
			begin := sanitizeLabel(id) + "_begin"
			end := sanitizeLabel(id) + "_end"
			asm.WriteString(fmt.Sprintf("%s:\n", begin))
			// test byte and jump to end if zero
			asm.WriteString("\tmov al, BYTE PTR [r12]\n")
			asm.WriteString("\ttest al, al\n")
			asm.WriteString(fmt.Sprintf("\tjz %s\n", end))
		case ']':
			if len(loopStack) == 0 {
				log.Fatalf("Unmatched ']' at source index %d", i)
			}
			id := loopStack[len(loopStack)-1]
			loopStack = loopStack[:len(loopStack)-1]
			begin := sanitizeLabel(id) + "_begin"
			end := sanitizeLabel(id) + "_end"
			// jump back to begin if byte != 0
			asm.WriteString("\tmov al, BYTE PTR [r12]\n")
			asm.WriteString("\ttest al, al\n")
			asm.WriteString(fmt.Sprintf("\tjnz %s\n", begin))
			asm.WriteString(fmt.Sprintf("%s:\n", end))
		default:
			// ignore any other characters (including whitespace / comments)
		}
	}

	if len(loopStack) != 0 {
		log.Fatalf("Unmatched '[' (stack not empty), top id=%d", loopStack[len(loopStack)-1])
	}

	// Epilogue: return 0
	asm.WriteString("\tmov eax, 0\n")
	asm.WriteString("\tpop rbp\n")
	asm.WriteString("\tret\n")

	// BSS tape allocation
	asm.WriteString("\t.section .bss\n")
	asm.WriteString("\t.align 8\n")
	asm.WriteString("tape:\n")
	asm.WriteString("\t.zero 30000\n")

	asmText := asm.String()

	// Write assembly to a temporary .s file
	tmpS := outExe + ".s"
	if err := ioutil.WriteFile(tmpS, []byte(asmText), 0644); err != nil {
		log.Fatalf("write asm file: %v", err)
	}
	defer func() {
		// optionally remove the .s file on success; keep if user wants to inspect
		// os.Remove(tmpS)
	}()

	// Assemble & link using gcc
	// -no-pie to avoid PIE-related relocation issues and to have a simple executable layout
	cmd := exec.Command("gcc", "-no-pie", tmpS, "-o", outExe)
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	if err := cmd.Run(); err != nil {
		log.Fatalf("gcc failed: %v", err)
	}

	fmt.Printf("Built executable: %s\n", outExe)

	// // Optionally, run the produced executable and stream output to the current process's stdout.
	// // We'll run it and hook stdin/stdout/stderr so the bf program can interact.
	// prog := exec.Command("./" + outExe)
	// prog.Stdin = os.Stdin
	// var outBuf bytes.Buffer
	// prog.Stdout = &outBuf
	// prog.Stderr = os.Stderr
	// if err := prog.Run(); err != nil {
	// 	// If the program returns non-zero, still print captured output and error
	// 	fmt.Print(outBuf.String())
	// 	log.Fatalf("running program failed: %v", err)
	// }
	// // print captured output
	// fmt.Print(outBuf.String())
}
