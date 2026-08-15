package drive

import "testing"

func TestLooksLikeCollectionTitle(t *testing.T) {
	yes := []string{
		"经典科幻《钢铁侠3部全》1080p.BD中英双字",
		"《复仇者联盟四部曲》",
		"速度与激情合集",
		"哈利波特1-8",
		"指环王三部曲",
	}
	no := []string{
		"2026科幻惊悚《灵魂伴侣》1080p.HD中英双字",
		"《钢铁侠2》",
		"不能错过的只有你2",
	}
	for _, s := range yes {
		if !looksLikeCollectionTitle(s) {
			t.Errorf("want collection: %q", s)
		}
	}
	for _, s := range no {
		if looksLikeCollectionTitle(s) {
			t.Errorf("not collection: %q", s)
		}
	}
}

func TestSeriesBaseFromTitle(t *testing.T) {
	got := seriesBaseFromTitle("经典科幻《钢铁侠3部全》1080p.BD中英双字")
	if got != "钢铁侠" {
		t.Fatalf("series base = %q, want 钢铁侠", got)
	}
}

func TestInferMovieTitle(t *testing.T) {
	page := "经典科幻《钢铁侠3部全》1080p.BD中英双字"
	cases := []struct {
		name, pth, want string
	}{
		{"1.mp4", "/6v下载/电影/" + page + "/1.mp4", "钢铁侠1"},
		{"2.mkv", "/x/" + page + "/2.mkv", "钢铁侠2"},
		{"03.mp4", "", "钢铁侠3"},
		{"钢铁侠1.1080p.BD中英双字.mp4", "", "钢铁侠1"},
		{"钢铁侠2.1080p.mkv", "", "钢铁侠2"},
		{"Iron Man 3 1080p.mkv", "", "Iron Man 3"},
		{"video.mp4", "/pack/3/video.mp4", "钢铁侠3"},
		{"钢铁侠3部全.1080p.BD中英双字.mp4", "", "钢铁侠"},
		{"钢铁侠3.1080p.国英双语.BD中英双字[66影视www.66Ys.Co].mp4", "", "钢铁侠3"},
		{"复仇者联盟4：终局之战.1080p.国英双语.BD中英双字[最新电影www.66e.cc].mp4", "", "复仇者联盟4 终局之战"},
		{"复仇者联盟2：奥创纪元.720p.国英双语.BD中英双字[最新电影www.6vhao.tv].mp4", "", "复仇者联盟2 奥创纪元"},
	}
	for _, c := range cases {
		got := inferMovieTitle(c.name, c.pth, page)
		if got != c.want {
			t.Errorf("infer(%q, %q) = %q, want %q", c.name, c.pth, got, c.want)
		}
	}
}

func TestShouldSplitMovieCollection(t *testing.T) {
	page := "经典科幻《钢铁侠3部全》1080p.BD中英双字"
	videos := []*File{
		{Name: "1.mp4", Path: "/6v下载/电影/" + page + "/1.mp4"},
		{Name: "2.mp4", Path: "/6v下载/电影/" + page + "/2.mp4"},
		{Name: "3.mp4", Path: "/6v下载/电影/" + page + "/3.mp4"},
	}
	if !shouldSplitMovieCollection(videos, page) {
		t.Fatal("1/2/3.mp4 in 3部全 should split")
	}
	named := []*File{
		{Name: "钢铁侠1.1080p.mp4", Path: "/x/a.mp4"},
		{Name: "钢铁侠2.1080p.mp4", Path: "/x/b.mp4"},
		{Name: "钢铁侠3.1080p.mp4", Path: "/x/c.mp4"},
	}
	if !shouldSplitMovieCollection(named, page) {
		t.Fatal("named sequels should split")
	}
	same := []*File{
		{Name: "灵魂伴侣.cd1.mkv", Path: "/x/cd1.mkv"},
		{Name: "灵魂伴侣.cd2.mkv", Path: "/x/cd2.mkv"},
	}
	if shouldSplitMovieCollection(same, "灵魂伴侣 (2026)") {
		t.Fatal("same movie CD1/CD2 must not split")
	}
	single := []*File{{Name: "灵魂伴侣.mkv", Path: "/x/a.mkv"}}
	if shouldSplitMovieCollection(single, "灵魂伴侣 (2026)") {
		t.Fatal("single movie must not split")
	}
}

func TestMagnetLooksLikeMoviePart(t *testing.T) {
	page := "经典科幻《钢铁侠3部全》1080p.BD中英双字"
	if !magnetLooksLikeMoviePart("钢铁侠1.1080p.BD中英双字", page) {
		t.Fatal("钢铁侠1 should be a movie part")
	}
	if !magnetLooksLikeMoviePart("钢铁侠2", page) {
		t.Fatal("钢铁侠2 should be a movie part")
	}
	if magnetLooksLikeMoviePart("钢铁侠3部全.1080p.BD中英双字", page) {
		t.Fatal("collection magnet is not a single part")
	}
}
