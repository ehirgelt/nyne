// Acmego watches acme for .go files being written.
// Each time a .go file is written, acmego checks whether the
// import block needs adjustment. If so, it makes the changes
// in the window body but does not write the file.
package nyne

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"

	"9fans.net/go/acme"
	"git.sr.ht/~danieljamespost/nyne/pkg/util/config"
)

func New(conf *config.Config) {
	l, err := acme.Log()
	if err != nil {
		log.Fatal(err)
	}
	for {
		event, err := l.Read()
		if err != nil {
			log.Fatal(err)
		}
		if event.Name != "" && event.Op == "put" {
			for _, spec := range conf.Spec {
				for _, ext := range spec.Ext {
					if strings.HasSuffix(event.Name, ext) {
						for _, cmd := range spec.Cmd {
							args := replaceName(cmd.Args, event.Name)
							reformat(event.ID, event.Name, cmd.Exec, args, ext)
					    		event, err = l.Read()
					    		if err != nil {
					    			log.Fatal(err)
					    		}
				    		}
				    	}
			    	}
		    	}
		}
	}
}

func replaceName(arr []string, name string) []string {
	newArr := make([]string, len(arr))
	for idx, str := range arr {
		if str == "$NAME" {
			newArr[idx] = name
		} else {
			newArr[idx] = arr[idx]
		}
	}
	return newArr
}

func reformat(id int, name string, x string, args []string, ext string) {
	w, err := acme.Open(id, nil)
	if err != nil {
		log.Print(err)
		return
	}
	defer w.CloseFiles()

	old, err := ioutil.ReadFile(name)
	if err != nil {
		return
	}
	new, err := exec.Command(x, args...).CombinedOutput()
	if err != nil {
		if strings.Contains(string(new), "fatal error") {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n%s", x, name, err, new)
		} else {
			fmt.Fprintf(os.Stderr, "%s", new)
		}
		return
	}
	
	if ext != ".go" {
		w.Write("ctl", []byte("clean"))
		w.Write("ctl", []byte("get"))	
		return
	}
	
	if bytes.Equal(old, new) {
		return
	}

	if ext == ".go" {
		oldTop, err := readImports(bytes.NewReader(old), true)
		if err != nil {
			//log.Print(err)
			return
		}
		newTop, err := readImports(bytes.NewReader(new), true)
		if err != nil {
			//log.Print(err)
			return
		}
		if bytes.Equal(oldTop, newTop) {
			return
		}
		w.Addr("0,#%d", utf8.RuneCount(oldTop))
		w.Write("data", newTop)
		return
	}

	f, err := ioutil.TempFile("", "nyne")
	if err != nil {
		log.Print(err)
		return
	}
	if _, err := f.Write(new); err != nil {
		log.Print(err)
		return
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp)

	diff, _ := exec.Command("9", "diff", name, tmp).CombinedOutput()

	latest, err := w.ReadAll("body")
	if err != nil {
		log.Print(err)
		return
	}
	if !bytes.Equal(old, latest) {
		log.Printf("skipped update to %s: window modified since Put\n", name)
		return
	}
	w.Write("ctl", []byte("mark"))
	w.Write("ctl", []byte("nomark"))
	diffLines := strings.Split(string(diff), "\n")
	for i := len(diffLines) - 1; i >= 0; i-- {
		line := diffLines[i]
		fmt.Println(line);
		if line == "" {
			continue
		}
		if line[0] == '<' || line[0] == '-' || line[0] == '>' {
			continue
		}
		j := 0
		for j < len(line) && line[j] != 'a' && line[j] != 'c' && line[j] != 'd' {
			j++
		}
		if j >= len(line) {
			log.Printf("cannot parse diff line: %q", line)
			break
		}
		oldStart, oldEnd := parseSpan(line[:j])
		newStart, newEnd := parseSpan(line[j+1:])
		if oldStart == 0 || newStart == 0 {
			continue
		}
		switch line[j] {
		case 'a':
			err := w.Addr("%d+#0", oldStart)
			if err != nil {
				log.Print(err)
				break
			}
			w.Write("data", findLines(new, newStart, newEnd))
		case 'c':
			err := w.Addr("%d,%d", oldStart, oldEnd)
			if err != nil {
				log.Print(err)
				break
			}
			w.Write("data", findLines(new, newStart, newEnd))
		case 'd':
			err := w.Addr("%d,%d", oldStart, oldEnd)
			if err != nil {
				log.Print(err)
				break
			}
		}
	}
	
	// update buffer
	w.Write("ctl", []byte("clean"))
	w.Write("ctl", []byte("get"))

}

func parseSpan(text string) (start, end int) {
	i := strings.Index(text, ",")
	if i < 0 {
		n, err := strconv.Atoi(text)
		if err != nil {
			log.Printf("cannot parse span %q", text)
			return 0, 0
		}
		return n, n
	}
	start, err1 := strconv.Atoi(text[:i])
	end, err2 := strconv.Atoi(text[i+1:])
	if err1 != nil || err2 != nil {
		log.Printf("cannot parse span %q", text)
		return 0, 0
	}
	return start, end
}

func findLines(text []byte, start, end int) []byte {
	i := 0

	start--
	for ; i < len(text) && start > 0; i++ {
		if text[i] == '\n' {
			start--
			end--
		}
	}
	startByte := i
	for ; i < len(text) && end > 0; i++ {
		if text[i] == '\n' {
			end--
		}
	}
	endByte := i
	return text[startByte:endByte]
}
