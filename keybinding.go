package main

import (
	"encoding/binary"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/stretto-editor/gocui"
)

var currentFile string

const fileMode = "file"
const editMode = "edit"
const cmdMode = "cmd"

type input struct {
	channel chan int
	content string
}

var in input

func initModes(g *gocui.Gui) {
	g.SetMode(cmdMode)
	g.SetMode(fileMode)
	g.SetMode(editMode)
}

func initKeybindings(g *gocui.Gui) error {
	in.channel = make(chan int)

	var keyBindings = []struct {
		m string
		v string
		k gocui.Key
		h gocui.KeybindingHandler
	}{
		{m: fileMode, v: "main", k: gocui.KeyTab, h: switchModeTo(editMode)},
		{m: editMode, v: "main", k: gocui.KeyTab, h: switchModeTo(fileMode)},
		{m: fileMode, v: "", k: gocui.KeyCtrlC, h: quit},
		{m: editMode, v: "", k: gocui.KeyCtrlC, h: quit},
		{m: fileMode, v: "", k: gocui.KeyHome, h: cursorHome},
		{m: fileMode, v: "", k: gocui.KeyEnd, h: cursorEnd},
		{m: fileMode, v: "", k: gocui.KeyPgup, h: goPgUp},
		{m: fileMode, v: "", k: gocui.KeyPgdn, h: goPgDown},
		{m: fileMode, v: "", k: gocui.KeyCtrlT, h: currTopViewHandler("cmdline")},
		{m: fileMode, v: "main", k: gocui.KeyCtrlS, h: save},
		{m: fileMode, v: "main", k: gocui.KeyCtrlF, h: search},
		{m: fileMode, v: "main", k: gocui.KeyCtrlA, h: getInput},
		{m: fileMode, v: "main", k: gocui.KeyCtrlC, h: copy},
		{m: fileMode, v: "main", k: gocui.KeyCtrlV, h: paste},
		{m: fileMode, v: "main", k: gocui.KeyCtrlP, h: replace},
		{m: fileMode, v: "cmdline", k: gocui.KeyCtrlT, h: currTopViewHandler("main")},
		{m: fileMode, v: "inputline", k: gocui.KeyEnter, h: validateInput},
	}

	for _, kb := range keyBindings {
		if err := g.SetKeybinding(kb.m, kb.v, kb.k, gocui.ModNone, kb.h); err != nil {
			return err
		}
	}

	g.SetCurrentMode(fileMode)

	return nil
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

func getInput(g *gocui.Gui, v *gocui.View) error {
	go functionnality(g, v)
	return nil
}

func functionnality(g *gocui.Gui, v *gocui.View) error {
	currTopViewHandler("inputline")(g, v)
	g.CurrentView().MoveCursor(0, 0, false)
	in.channel <- 1
	for _, c := range in.content {
		v.EditWrite(c)
	}
	return nil
}

func validateInput(g *gocui.Gui, v *gocui.View) error {
	str := v.Buffer()
	if len(str) < 2 {
		in.content = ""
	} else {
		in.content = str[:len(str)-2]
	}
	v.Clear()
	if err := currTopViewHandler("main")(g, v); err != nil {
		return err
	}
	g.CurrentView().MoveCursor(0, 0, false) // Bad way to update
	<-in.channel
	return nil
}

func switchModeTo(name string) gocui.KeybindingHandler {
	return func(g *gocui.Gui, v *gocui.View) error {
		if err := g.SetCurrentMode(name); err != nil {
			return err
		}
		if v.Name() == "cmdline" {
			v.SetCursor(0, 0)
			v.Clear()
		}
		if name == "cmd" {
			g.SetCurrentView("cmdline")
		}
		return nil
	}
}

func currTopViewHandler(name string) gocui.KeybindingHandler {
	return func(g *gocui.Gui, v *gocui.View) error {
		if err := g.SetCurrentView(name); err != nil {
			return err
		}
		if _, err := g.SetViewOnTop(name); err != nil {
			return err
		}
		return nil
	}
}

func cursorHome(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		cx, _ := v.Cursor()
		v.MoveCursor(-cx, 0, true)
	}
	return nil
}

func cursorEnd(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		cx, cy := v.Cursor()
		l, _ := v.Line(cy)
		v.MoveCursor(len(l)-cx, 0, true)
	}
	return nil
}

func goPgUp(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ox, oy := v.Origin()
		_, y := v.Size()
		yOffset := oy - y
		if yOffset < 0 {
			if err := v.SetOrigin(ox, 0); err != nil {
				return err
			}
			if oy == 0 {
				_, cy := v.Cursor()
				v.MoveCursor(0, -cy, false)
			} else {
				v.MoveCursor(0, 0, false)
			}
		} else {
			v.SetOrigin(ox, yOffset)
			v.MoveCursor(0, 0, false)
		}
	}
	return nil
}

func goPgDown(g *gocui.Gui, v *gocui.View) error {
	if v != nil {
		ox, oy := v.Origin()
		_, y := v.Size()
		_, cy := v.Cursor()
		if y >= v.BufferSize() {
			v.MoveCursor(0, y-cy, false)
		} else if oy >= v.BufferSize()-y {
			v.MoveCursor(0, y, false)
		} else if oy+2*y >= v.BufferSize() {
			v.SetOrigin(ox, v.BufferSize()-y+1)
			v.MoveCursor(0, 0, false)
		} else {
			v.SetOrigin(ox, oy+y)
			v.MoveCursor(0, 0, false)
		}
	}
	return nil
}

func saveMain(g *gocui.Gui, v *gocui.View, filename string) error {
	if filename == "" {
		return nil
	}
	f, err := os.OpenFile(filename, os.O_WRONLY, 0666)
	if err != nil {
		if strings.HasSuffix(err.Error(), "permission denied") {
			return nil
		}
		return err
	}
	defer f.Close()

	p := make([]byte, 5)
	v.Rewind()
	var size int64 = -1
	for {
		n, err := v.Read(p)
		size += int64(n)
		if n > 0 {
			if _, er := f.Write(p[:n]); err != nil {
				return er
			}
		}
		if err == io.EOF {
			f.Truncate(size * int64(binary.Size(p[0])))
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func search(g *gocui.Gui, v *gocui.View) error {
	go searchInteractive(g, v)
	return nil
}

func searchInteractive(g *gocui.Gui, v *gocui.View) (bool, error) {
	currTopViewHandler("inputline")(g, v)
	g.CurrentView().MoveCursor(0, 0, false)
	in.channel <- 1

	if len(in.content) > 0 {

		var s string
		var err error
		var sameline = 1

		x, y := v.Cursor()

		for i := 0; err == nil; i++ {
			s, err = v.Line(y + i)
			if err == nil {
				// size of line is long enough to move the cursor
				if x < len(s)-1 {
					indice := strings.Index(s[x+sameline:], in.content)

					// existing element on this line
					if indice >= 0 {
						if sameline == 0 {
							x, y = v.Cursor()
							v.MoveCursor(indice+sameline-x, i, false)
						} else {
							v.MoveCursor(indice+sameline, i, false)
						}
						return true, nil
					}
				}
				x = 0
				sameline = 0
			}
		}
	}
	return false, nil
}

func save(g *gocui.Gui, v *gocui.View) error {
	return saveMain(g, v, currentFile)
}

func copy(g *gocui.Gui, v *gocui.View) error {
	//http://stackoverflow.com/questions/10781516/how-to-pipe-several-commands-in-go
	if runtime.GOOS == "windows" {
		return nil
	}
	c1 := exec.Command("xsel")
	c2 := exec.Command("xclip", "-selection", "c")
	r, w := io.Pipe()
	c1.Stdout = w
	c2.Stdin = r

	if err := c1.Start(); err != nil {
		return err
	}

	if err := c2.Start(); err != nil {
		return err
	}
	if err := c1.Wait(); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	if err := c2.Wait(); err != nil {
		return err
	}
	return nil
}

func paste(g *gocui.Gui, v *gocui.View) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	out, err := exec.Command("xsel", "-b").Output()
	s := string(out)
	if err != nil {
		return err
	}
	for _, r := range s {
		if rune(r) == '\n' {
			v.EditNewLine()
		} else {
			v.EditWrite(rune(r))
		}
	}
	return nil
}

func replace(g *gocui.Gui, v *gocui.View) error {
	go replaceInteractive(g, v)
	return nil
}

func replaceInteractive(g *gocui.Gui, v *gocui.View) error {
	if found, err := searchInteractive(g, v); err != nil || found == false {
		if err != nil {
			return err
		}
		return nil
	}

	searched := in.content

	currTopViewHandler("inputline")(g, v)
	g.CurrentView().MoveCursor(0, 0, false)
	in.channel <- 1

	for i := 0; i < len(searched); i++ {
		v.EditDelete(false)
	}

	replaced := in.content

	for _, c := range replaced {
		v.EditWrite(c)
	}

	return nil
}
