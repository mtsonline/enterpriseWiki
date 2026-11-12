package content_forms

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Checks of uploaded files: the type is taken from the content (and must
// fit the extension and the accepted kinds), Office files with macros and
// PDF files with active content are refused unless the field allows them,
// and an optional virus scanner (ClamAV clamd, setting "clamd") checks
// every file. A scanner that is configured but cannot be reached refuses
// the upload (fail closed).

// ErrRejected is a refused file (the message is shown to the submitter).
type ErrRejected struct{ Code, Name string }

func (e *ErrRejected) Error() string { return e.Code + ": " + e.Name }

// sniff returns the kind of a file by its content.
func sniff(head []byte) string {
	switch {
	case bytes.HasPrefix(head, []byte("%PDF-")):
		return "pdf"
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")), bytes.HasPrefix(head, []byte("\xff\xd8\xff")),
		bytes.HasPrefix(head, []byte("GIF87a")), bytes.HasPrefix(head, []byte("GIF89a")),
		len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP")):
		return "image"
	case bytes.HasPrefix(head, []byte("PK\x03\x04")):
		return "zip" // docx, xlsx, pptx, odt … or a zip archive
	case bytes.HasPrefix(head, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")):
		return "ole" // doc, xls, ppt
	case bytes.HasPrefix(head, []byte("{\\rtf")):
		return "rtf"
	case bytes.HasPrefix(head, []byte("ID3")), len(head) > 2 && head[0] == 0xff && head[1]&0xe0 == 0xe0,
		bytes.HasPrefix(head, []byte("OggS")), len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WAVE")):
		return "audio"
	case len(head) >= 12 && bytes.Equal(head[4:8], []byte("ftyp")):
		return "mp4" // mp4, m4a
	case bytes.HasPrefix(head, []byte("\x1a\x45\xdf\xa3")):
		return "video" // webm
	}
	ct := http.DetectContentType(head)
	if strings.HasPrefix(ct, "text/plain") {
		return "text"
	}
	return ""
}

// content kinds an extension may have
var extContent = map[string][]string{
	".png": {"image"}, ".jpg": {"image"}, ".jpeg": {"image"}, ".gif": {"image"}, ".webp": {"image"},
	".pdf": {"pdf"}, ".docx": {"zip"}, ".xlsx": {"zip"}, ".pptx": {"zip"}, ".odt": {"zip"}, ".ods": {"zip"}, ".odp": {"zip"},
	".docm": {"zip"}, ".xlsm": {"zip"}, ".pptm": {"zip"},
	".doc": {"ole"}, ".xls": {"ole"}, ".ppt": {"ole"}, ".rtf": {"rtf"}, ".zip": {"zip"},
	".txt": {"text"}, ".csv": {"text"}, ".md": {"text"},
	".mp3": {"audio"}, ".ogg": {"audio"}, ".wav": {"audio"}, ".m4a": {"mp4", "audio"}, ".mp4": {"mp4"}, ".webm": {"video"},
}

// accepted reports whether a file name is accepted by the field.
func (x *Field) accepted(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, a := range x.Accept {
		if a == ext || slices.Contains(FileKinds[a], ext) {
			return true
		}
	}
	return false
}

// checkFile checks one uploaded file (stored at path) for field x.
func (p *Plugin) checkFile(ctx context.Context, x *Field, name, path string) error {
	ext := strings.ToLower(filepath.Ext(name))
	if !x.accepted(name) {
		return &ErrRejected{"file_type", name}
	}
	f, err := os.Open(path) // #nosec G304 -- temporary file of this upload
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 4096)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	kind := sniff(head)
	if want, ok := extContent[ext]; ok && !slices.Contains(want, kind) {
		return &ErrRejected{"file_content", name} // content does not match the extension
	} else if !ok && (kind == "" || kind == "zip" || kind == "ole") {
		return &ErrRejected{"file_content", name} // own extensions: only plain kinds
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	switch kind {
	case "zip":
		if !x.AllowMacros || ext == ".zip" {
			if err := checkZip(f, st.Size(), x.AllowMacros); err != nil {
				return &ErrRejected{err.Error(), name}
			}
		}
	case "ole":
		if !x.AllowMacros {
			if found, err := oleHasMacros(f); err != nil || found {
				return &ErrRejected{"macros", name}
			}
		}
	case "pdf":
		if !x.AllowActive {
			if found, err := pdfActive(f); err != nil || found {
				return &ErrRejected{"pdf_active", name}
			}
		}
	case "rtf":
		if !x.AllowMacros {
			if found, err := contains(f, [][]byte{[]byte(`\objdata`), []byte(`\objupdate`)}); err != nil || found {
				return &ErrRejected{"macros", name} // embedded objects (exploits)
			}
		}
	}
	return p.virusScan(ctx, f, name)
}

// checkZip refuses macros (vbaProject.bin, ODF Basic), nested archives and
// zip bombs.
func checkZip(f *os.File, size int64, allowMacros bool) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return errors.New("file_content")
	}
	if len(zr.File) > 10000 {
		return errors.New("archive")
	}
	var total uint64
	for _, e := range zr.File {
		total += e.UncompressedSize64
		if total > 2<<30 || e.UncompressedSize64 > 0 && e.CompressedSize64 > 0 && e.UncompressedSize64/e.CompressedSize64 > 200 {
			return errors.New("archive") // zip bomb
		}
		n := strings.ToLower(e.Name)
		if strings.Contains(n, "..") || strings.HasPrefix(n, "/") {
			return errors.New("archive")
		}
		if !allowMacros && (strings.HasSuffix(n, "vbaproject.bin") || strings.HasPrefix(n, "basic/") || strings.HasSuffix(n, ".bas") ||
			strings.HasSuffix(n, "vbadata.xml") || strings.Contains(n, "activex")) {
			return errors.New("macros")
		}
		if ext := filepath.Ext(n); slices.Contains([]string{".exe", ".dll", ".js", ".vbs", ".bat", ".cmd", ".ps1", ".scr", ".jar", ".hta", ".lnk", ".msi"}, ext) {
			return errors.New("archive") // executables inside
		}
	}
	return nil
}

// oleHasMacros looks for the VBA storage names of Office 97–2003 files
// (stored in UTF-16 in the directory of the compound file).
func oleHasMacros(f *os.File) (bool, error) {
	u16 := func(s string) []byte {
		b := make([]byte, 0, 2*len(s))
		for _, r := range s {
			b = binary.LittleEndian.AppendUint16(b, uint16(r)) // #nosec G115 -- ASCII
		}
		return b
	}
	return contains(f, [][]byte{u16("_VBA_PROJECT"), u16("VBA"), u16("Macros")})
}

// pdfActive finds JavaScript, launch actions and embedded files in a PDF.
func pdfActive(f *os.File) (bool, error) {
	return contains(f, [][]byte{[]byte("/JavaScript"), []byte("/JS"), []byte("/Launch"), []byte("/EmbeddedFile"),
		[]byte("/RichMedia"), []byte("/XFA"), []byte("/AA"), []byte("/SubmitForm"), []byte("/ImportData")})
}

// contains searches the whole file for any of the needles.
func contains(f *os.File, needles [][]byte) (bool, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false, err
	}
	longest := 0
	for _, n := range needles {
		longest = max(longest, len(n))
	}
	r := bufio.NewReaderSize(f, 64<<10)
	buf := make([]byte, 0, 128<<10)
	chunk := make([]byte, 64<<10)
	for {
		n, err := r.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for _, nd := range needles {
			if bytes.Contains(buf, nd) {
				return true, nil
			}
		}
		if len(buf) > longest {
			buf = append(buf[:0], buf[len(buf)-longest:]...)
		}
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
}

// clamd returns the address of the virus scanner ("" = none).
func (p *Plugin) clamd() string {
	if p.host == nil {
		return ""
	}
	addr, _ := p.host.Settings()["clamd"].(string)
	return strings.TrimSpace(addr)
}

// virusScan sends the file to clamd (setting "clamd": "tcp://host:3310" or
// "unix:/path/clamd.sock"); without the setting nothing is scanned.
func (p *Plugin) virusScan(ctx context.Context, f *os.File, name string) error {
	addr := p.clamd()
	if addr == "" {
		return nil
	}
	network, address := "tcp", strings.TrimPrefix(addr, "tcp://")
	if a, ok := strings.CutPrefix(addr, "unix:"); ok {
		network, address = "unix", a
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, network, address)
	if err != nil {
		p.host.Log.Error("virus scanner not reachable", "err", err)
		return &ErrRejected{"scan_unavailable", name}
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Minute))
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return &ErrRejected{"scan_unavailable", name}
	}
	buf := make([]byte, 32<<10)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			var l [4]byte
			binary.BigEndian.PutUint32(l[:], uint32(n)) // #nosec G115 -- at most 32 KiB
			if _, err := conn.Write(append(l[:], buf[:n]...)); err != nil {
				return &ErrRejected{"scan_unavailable", name}
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return &ErrRejected{"scan_unavailable", name}
	}
	reply, _ := io.ReadAll(io.LimitReader(conn, 4096))
	res := strings.TrimRight(string(reply), "\x00\n ")
	switch {
	case strings.HasSuffix(res, "OK"):
		return nil
	case strings.HasSuffix(res, "FOUND"):
		p.host.Log.Warn("virus found in form upload", "file", name, "result", res)
		return &ErrRejected{"virus", name}
	}
	p.host.Log.Error("virus scanner error", "result", res)
	return &ErrRejected{"scan_unavailable", name}
}
