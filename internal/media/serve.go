package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

func imageConfig(b []byte) (image.Config, string, error) {
	return image.DecodeConfig(bytes.NewReader(b))
}

// lines returns the text of a watermark.
func lines(spec WatermarkSpec, name string, t time.Time) []string {
	var out []string
	if spec.User && name != "" {
		out = append(out, name)
	}
	if spec.Date {
		out = append(out, t.Format("2006-01-02 15:04"))
	}
	if spec.Comment != "" {
		out = append(out, spec.Comment)
	}
	return out
}

// mark burns a watermark into an image or (as page images) a PDF.
func (s *Service) mark(ctx context.Context, data []byte, spec WatermarkSpec, name string, t time.Time) ([]byte, error) {
	text := lines(spec, name, t)
	cfg, _, err := imageConfig(data)
	if err == nil && cfg.Width > 0 {
		img, format, err := Decode(data)
		if err != nil {
			return nil, apperr.BadRequest("cannot read the image: %v", err)
		}
		out, err := Watermark(img, text)
		if err != nil {
			return nil, err
		}
		if format == "webp" {
			format = "png"
		}
		b, _, err := Encode(out, format)
		return b, err
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		return markPDF(ctx, data, text)
	}
	return nil, apperr.BadRequest("watermarks are available for images and PDF files")
}

// Delivery is what a caller gets for a medium.
type Delivery struct {
	Data     []byte
	Mime     string
	Name     string
	Inline   bool
	ViewOnly bool
	// Pages of a PDF shown as images (view only / with watermark).
	Pages int
}

// DeliverInput selects what to deliver.
type DeliverInput struct {
	Download bool
	Version  int // 0: current (others need the right "edit")
	Width    int // images: scaled down to this width (thumbnails)
	Page     int // PDFs shown as images: page (1-based); 0: the PDF or the page count
}

// Deliver returns the file of a medium for the caller, with the watermark
// and restrictions its settings require.
func (s *Service) Deliver(ctx context.Context, id int64, in DeliverInput) (*Delivery, error) {
	want := View
	if in.Download {
		want |= Download
	}
	m, perms, err := s.require(ctx, id, want)
	if err != nil {
		return nil, err
	}
	hash, mime := m.hash, m.Mime
	if in.Version > 0 && in.Version != m.Version {
		if perms&Edit == 0 {
			return nil, apperr.ErrForbidden
		}
		if err := s.pool.QueryRow(ctx, `SELECT hash, mime FROM media_versions WHERE media_id=$1 AND version=$2`, id, in.Version).Scan(&hash, &mime); err != nil {
			return nil, apperr.NotFound("version")
		}
	}
	data, err := s.read(hash)
	if err != nil {
		return nil, err
	}
	d := &Delivery{Mime: mime, Name: m.Name, Inline: !in.Download, ViewOnly: perms&Download == 0}

	// which mark, if any
	var markName string
	var markTime time.Time
	marked := false
	switch m.Watermark.Mode {
	case WMUploaderView:
		if !in.Download {
			marked, markName, markTime = true, s.userName(ctx, m.CreatedBy), m.CreatedAt
		}
	case WMViewerView, WMViewerAll:
		if !in.Download || m.Watermark.Mode == WMViewerAll {
			marked, markName, markTime = true, viewerName(ctx), time.Now()
		}
	}
	isPDF := mime == "application/pdf"
	isImage := editable(mime)
	// PDFs that must not leave as file are shown as page images
	pdfPages := isPDF && !in.Download && (marked || d.ViewOnly)
	if !isImage && !isPDF && (marked || (d.ViewOnly && !in.Download)) && !strings.HasPrefix(mime, "video/") && !strings.HasPrefix(mime, "audio/") {
		if marked {
			return nil, apperr.BadRequest("this file type cannot be shown with a watermark")
		}
		if !strings.HasPrefix(mime, "text/plain") {
			return nil, apperr.New(415, "not_viewable", "this file cannot be shown in the browser")
		}
	}
	day := markTime.Format("2006-01-02")
	key := fmt.Sprintf("%s|%v|%s|%s|%s|%d|%d", hash, marked, m.Watermark.Mode, markName, day, in.Width, in.Page)
	if marked {
		key += "|" + strings.Join(lines(m.Watermark, markName, markTime), "|")
	}

	switch {
	case pdfPages:
		n, err := pdfPageCount(ctx, s, hash, data)
		if err != nil {
			return nil, err
		}
		d.Pages = n
		if in.Page <= 0 {
			d.Data, d.Mime = nil, "application/json"
			return d, nil
		}
		if in.Page > n {
			return nil, apperr.NotFound("page")
		}
		if b, ok := s.files.CacheGet(key); ok {
			d.Data, d.Mime = b, "image/jpeg"
			return d, nil
		}
		img, err := pdfPage(ctx, data, in.Page, in.Width)
		if err != nil {
			return nil, err
		}
		if marked {
			if img, err = Watermark(img, lines(m.Watermark, markName, markTime)); err != nil {
				return nil, err
			}
		}
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return nil, err
		}
		s.files.CachePut(key, buf.Bytes())
		d.Data, d.Mime = buf.Bytes(), "image/jpeg"
		return d, nil
	case marked && isPDF: // download of a PDF with the viewer's mark
		if b, ok := s.files.CacheGet(key); ok {
			d.Data = b
			return d, nil
		}
		b, err := markPDF(ctx, data, lines(m.Watermark, markName, markTime))
		if err != nil {
			return nil, err
		}
		s.files.CachePut(key, b)
		d.Data = b
		return d, nil
	case isImage && (marked || (in.Width > 0 && !in.Download)):
		if b, ok := s.files.CacheGet(key); ok {
			d.Data = b
			if marked && mime == "image/webp" || in.Width > 0 && mime == "image/webp" {
				d.Mime = "image/png"
			}
			return d, nil
		}
		img, format, err := Decode(data)
		if err != nil {
			return nil, err
		}
		if in.Width > 0 && !in.Download && in.Width < img.Bounds().Dx() {
			img = Resize(img, min(in.Width, 4000), 0)
		}
		if marked {
			if img, err = Watermark(img, lines(m.Watermark, markName, markTime)); err != nil {
				return nil, err
			}
		}
		if format == "webp" {
			format = "png"
		}
		b, mt, err := Encode(img, format)
		if err != nil {
			return nil, err
		}
		s.files.CachePut(key, b)
		d.Data, d.Mime = b, mt
		return d, nil
	}
	d.Data = data
	return d, nil
}

func viewerName(ctx context.Context) string {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return "Gast / guest"
	}
	n := p.DisplayName
	if n == "" {
		n = p.Username
	}
	if p.Email != "" {
		n += " <" + p.Email + ">"
	}
	return n
}

func (s *Service) userName(ctx context.Context, id *int64) string {
	if id == nil || s.UserName == nil {
		return ""
	}
	return s.UserName(ctx, *id)
}

// ---- PDF (poppler) ----

func pdfTool(name string) (string, error) {
	p, err := exec.LookPath(name)
	if err != nil {
		return "", apperr.New(501, "not_available", "PDF pages cannot be rendered on this server (poppler-utils missing)")
	}
	return p, nil
}

func withTempPDF(data []byte, f func(path string) error) error {
	dir, err := os.MkdirTemp("", "ewiki-pdf-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	p := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return err
	}
	return f(p)
}

func pdfPageCount(ctx context.Context, s *Service, hash string, data []byte) (int, error) {
	key := "pages|" + hash
	if b, ok := s.files.CacheGet(key); ok {
		if n, err := strconv.Atoi(string(b)); err == nil {
			return n, nil
		}
	}
	bin, err := pdfTool("pdfinfo")
	if err != nil {
		return 0, err
	}
	n := 0
	err = withTempPDF(data, func(p string) error {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		out, err := exec.CommandContext(cctx, bin, p).Output() // #nosec G204 -- fixed tool, generated temp path
		if err != nil {
			return apperr.BadRequest("cannot read the PDF")
		}
		for _, l := range strings.Split(string(out), "\n") {
			if v, ok := strings.CutPrefix(l, "Pages:"); ok {
				n, _ = strconv.Atoi(strings.TrimSpace(v))
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if n <= 0 || n > 5000 {
		return 0, apperr.BadRequest("cannot read the PDF")
	}
	s.files.CachePut(key, []byte(strconv.Itoa(n)))
	return n, nil
}

// pdfPage renders one page (width in pixels, default 1400).
func pdfPage(ctx context.Context, data []byte, page, width int) (image.Image, error) {
	bin, err := pdfTool("pdftoppm")
	if err != nil {
		return nil, err
	}
	if width <= 0 || width > 2400 {
		width = 1400
	}
	var img image.Image
	err = withTempPDF(data, func(p string) error {
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		pg := strconv.Itoa(page)
		out, err := exec.CommandContext(cctx, bin, "-f", pg, "-l", pg, "-scale-to-x", strconv.Itoa(width), "-scale-to-y", "-1", // #nosec G204 -- fixed tool and numeric arguments
			"-jpeg", "-singlefile", p, "-").Output()
		if err != nil {
			return apperr.BadRequest("cannot render the page")
		}
		img, _, err = Decode(out)
		return err
	})
	return img, err
}

// markPDF renders all pages with the watermark and builds a new PDF from
// the page images (text is no longer selectable - intended for protected
// copies).
func markPDF(ctx context.Context, data []byte, text []string) ([]byte, error) {
	bin, err := pdfTool("pdftoppm")
	if err != nil {
		return nil, err
	}
	var pages [][]byte
	var sizes [][2]int
	err = withTempPDF(data, func(p string) error {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		dir := filepath.Dir(p)
		if err := exec.CommandContext(cctx, bin, "-r", "110", "-jpeg", p, filepath.Join(dir, "page")).Run(); err != nil { // #nosec G204 -- fixed tool, generated temp paths
			return apperr.BadRequest("cannot render the PDF")
		}
		files, _ := filepath.Glob(filepath.Join(dir, "page-*.jpg"))
		if len(files) == 0 || len(files) > 2000 {
			return apperr.BadRequest("cannot render the PDF")
		}
		sortPages(files)
		for _, f := range files {
			b, err := os.ReadFile(f) // #nosec G304 -- file written by pdftoppm in our temp dir
			if err != nil {
				return err
			}
			img, _, err := Decode(b)
			if err != nil {
				return err
			}
			if img, err = Watermark(img, text); err != nil {
				return err
			}
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 82}); err != nil {
				return err
			}
			pages = append(pages, buf.Bytes())
			sizes = append(sizes, [2]int{img.Bounds().Dx(), img.Bounds().Dy()})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return imagesToPDF(pages, sizes, 110), nil
}

func sortPages(files []string) {
	num := func(f string) int {
		base := strings.TrimSuffix(filepath.Base(f), ".jpg")
		n, _ := strconv.Atoi(base[strings.LastIndex(base, "-")+1:])
		return n
	}
	for i := 1; i < len(files); i++ {
		for j := i; j > 0 && num(files[j]) < num(files[j-1]); j-- {
			files[j], files[j-1] = files[j-1], files[j]
		}
	}
}

// imagesToPDF writes a PDF with one JPEG per page (dpi: resolution of the
// images, for the page size).
func imagesToPDF(pages [][]byte, sizes [][2]int, dpi int) []byte {
	var b bytes.Buffer
	offsets := []int{}
	obj := func(n int, body string, stream []byte) {
		for len(offsets) < n {
			offsets = append(offsets, 0)
		}
		offsets[n-1] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\n", n, body)
		if stream != nil {
			b.WriteString("stream\n")
			b.Write(stream)
			b.WriteString("\nendstream\n")
		}
		b.WriteString("endobj\n")
	}
	b.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	n := len(pages)
	kids := make([]string, n)
	for i := range pages {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i*3)
	}
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>", nil)
	obj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n), nil)
	for i, img := range pages {
		w, h := sizes[i][0], sizes[i][1]
		pw, ph := float64(w)*72/float64(dpi), float64(h)*72/float64(dpi)
		pageN, contN, imgN := 3+i*3, 4+i*3, 5+i*3
		obj(pageN, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>", pw, ph, imgN, contN), nil)
		content := []byte(fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im0 Do Q", pw, ph))
		obj(contN, fmt.Sprintf("<< /Length %d >>", len(content)), content)
		obj(imgN, fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>", w, h, len(img)), img)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref)
	return b.Bytes()
}
