package server_test

import (
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
)

func TestBlogAndFAQ(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	guest := e.client()
	var site, blogCat, sub, faqCat, faqSec cat
	admin.must("POST", "/api/v1/categories", map[string]any{"titles": map[string]string{"en": "Site"}}, &site, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/categories/%d/acl", site.ID), map[string]any{"subject_type": "guest", "permissions": 1, "inherit": true}, nil, 201)
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": site.ID, "titles": map[string]string{"en": "News"}}, &blogCat, 201)

	// ------------------------------------------------------------ blog
	// without a source a category named like the blog is created (see TestShortURLsListingAndOverview)
	if code := admin.do("POST", "/api/v1/elements", map[string]any{"category_id": site.ID, "type": "content.blog", "title": "Blog"}, nil); code != 201 {
		t.Fatalf("blog without category: %d", code)
	}
	var blog elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": blogCat.ID, "type": "content.blog", "title": "Our blog",
		"body": map[string]any{"category_id": blogCat.ID, "recursive": true, "per_page": 2, "feed": true, "show_date": true, "show_author": true,
			"intro": map[string]string{"format": "markdown", "source": "Welcome <script>x</script>**all**"}}}, &blog, 201)
	for i, title := range []string{"First post", "Second post", "Third post"} {
		admin.must("POST", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts", blog.ID), map[string]any{"title": title, "lang": "en",
			"body": htmlBody(fmt.Sprintf("<p>Body %d about zebras</p>", i)), "tags": []string{"t" + fmt.Sprint(i%2)}}, nil, 201)
	}
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/categories", blog.ID), map[string]any{"titles": map[string]string{"en": "Events"}}, &sub, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts", blog.ID), map[string]any{"title": "Summer party", "category_id": sub.ID, "body": htmlBody("<p>Party</p>")}, nil, 201)
	if code := admin.do("POST", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts", blog.ID), map[string]any{"title": "Elsewhere", "category_id": site.ID}, nil); code != 400 {
		t.Fatalf("post outside the blog accepted: %d", code)
	}
	var page struct {
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
		Total int `json:"total"`
		Pages int `json:"pages"`
		Info  struct {
			CanCreate bool `json:"can_create"`
		} `json:"info"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts?page=1", blog.ID), nil, &page, 200)
	if page.Total != 4 || page.Pages != 2 || len(page.Items) != 2 || page.Items[0].Title != "Summer party" || page.Info.CanCreate {
		t.Fatalf("blog page: %+v", page)
	}
	page.Items = nil
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts?tag=t0", blog.ID), nil, &page, 200)
	if page.Total != 2 {
		t.Fatalf("tag filter: %+v", page)
	}
	var hits struct {
		Hits []struct {
			Title string `json:"title"`
		} `json:"hits"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.blog/elements/%d/posts?q=zebras", blog.ID), nil, &hits, 200)
	if len(hits.Hits) != 3 {
		t.Fatalf("search in blog: %+v", hits)
	}
	var view struct {
		HTML string `json:"html"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d", blog.ID), nil, &view, 200)
	if !strings.Contains(view.HTML, `data-ew-widget="blog"`) || !strings.Contains(view.HTML, "<strong>all</strong>") || strings.Contains(view.HTML, "<script") ||
		!strings.Contains(view.HTML, "Summer party") || strings.Contains(view.HTML, "First post") {
		t.Fatalf("server-rendered blog: %s", view.HTML)
	}
	resp, body := guest.raw(fmt.Sprintf("/api/v1/x/content.blog/elements/%d/feed?format=rss", blog.ID), nil)
	var rss struct {
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title string `xml:"title"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if resp.StatusCode != 200 || xml.Unmarshal([]byte(body), &rss) != nil || len(rss.Channel.Items) != 4 || rss.Channel.Title != "Our blog" {
		t.Fatalf("blog feed: %d %s", resp.StatusCode, body)
	}

	// ------------------------------------------------------------ FAQ
	admin.must("POST", "/api/v1/categories", map[string]any{"parent_id": site.ID, "titles": map[string]string{"en": "Help"}}, &faqCat, 201)
	var faq elem
	admin.must("POST", "/api/v1/elements", map[string]any{"category_id": faqCat.ID, "type": "content.faq", "title": "FAQ",
		"body": map[string]any{"category_id": faqCat.ID, "recursive": true, "numbering": true, "feed": false}}, &faq, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/categories", faq.ID), map[string]any{"titles": map[string]string{"en": "Accounts"}}, &faqSec, 201)
	var nested cat
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/categories", faq.ID), map[string]any{"parent_id": faqSec.ID, "titles": map[string]string{"en": "Passwords"}}, &nested, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/questions", faq.ID), map[string]any{"title": "Who answers questions?", "body": map[string]string{"format": "markdown", "source": "The **help desk**."}}, nil, 201)
	admin.must("POST", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/questions", faq.ID), map[string]any{"title": "How do I reset my password?", "category_id": nested.ID, "body": map[string]string{"format": "markdown", "source": "Use the reset link."}}, nil, 201)
	var faqData struct {
		Sections []struct {
			Title     string `json:"title"`
			Depth     int    `json:"depth"`
			Number    string `json:"number"`
			Questions []struct {
				Title string `json:"title"`
				HTML  string `json:"html"`
			} `json:"questions"`
		} `json:"sections"`
	}
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/questions", faq.ID), nil, &faqData, 200)
	if len(faqData.Sections) != 3 || len(faqData.Sections[0].Questions) != 1 || !strings.Contains(faqData.Sections[0].Questions[0].HTML, "<strong>help desk</strong>") ||
		faqData.Sections[2].Number != "1.1" || faqData.Sections[2].Questions[0].Title != "How do I reset my password?" {
		t.Fatalf("faq sections: %+v", faqData)
	}
	faqData.Sections = nil
	guest.must("GET", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/questions?q=reset", faq.ID), nil, &faqData, 200)
	if len(faqData.Sections) != 3 || len(faqData.Sections[0].Questions) != 0 || len(faqData.Sections[2].Questions) != 1 {
		t.Fatalf("faq search: %+v", faqData)
	}
	guest.must("GET", fmt.Sprintf("/api/v1/elements/%d", faq.ID), nil, &view, 200)
	if !strings.Contains(view.HTML, `<details class="ewq-section ewq-depth-1"`) || !strings.Contains(view.HTML, `<details class="ewq-q" id="q-`) ||
		!strings.Contains(view.HTML, `<span class="ewq-num">1.1.1</span> How do I reset my password?`) {
		t.Fatalf("server-rendered faq: %s", view.HTML)
	}
	if code := guest.do("GET", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/feed", faq.ID), nil, nil); code != 404 {
		t.Fatalf("disabled faq feed: %d", code)
	}
	// wrong element type
	if code := guest.do("GET", fmt.Sprintf("/api/v1/x/content.faq/elements/%d/questions", blog.ID), nil, nil); code != 404 {
		t.Fatalf("faq API on a blog: %d", code)
	}
}
