package drive

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// 合集页标题：3部全 / 三部曲 / 合集 / 1-3
var collectionTitleRe = regexp.MustCompile(`(?i)(\d+\s*部全|[一二三四五六七八九十]+部曲|部曲|合集|全集|系列|1\s*[-~～到至]\s*\d+)`)

var releaseTagRe = regexp.MustCompile(`(?i)(?:1080p|720p|2160p|4k|8k|bluray|blu-?ray|web-?dl|webrip|hdtv|hdr10|hdr|x264|x265|hevc|h\.?264|h\.?265|remux|bdrip|hdrip|dvdrip|hr-?hdtv|aac|ac3|dts|truehd|atmos|10bit|8bit|\bbd\b|\bhd\b|\bweb\b)`)

var zhReleaseTagRe = regexp.MustCompile(`(?:中英双字|中英字幕|中文字幕|英文字幕|国语|粤语|双语|特效|内封|外挂|高清|超清|蓝光|修复|枪版|中字)`)

// 6v / 66ys 水印：最新电影www.66e.cc、66影视www.66Ys.Co、6vhao.tv
var siteJunkRe = regexp.MustCompile(`(?i)(?:最新电影|66影视|6v电影|发布页)?\s*www[\s._-]*[a-z0-9-]+[\s._-]*(?:cc|tv|co|com|net|org)\b`)

var leftoverSiteRe = regexp.MustCompile(`(?i)\b(?:6vhao|66ys|66e|6v520|gvod)\b`)

var punctRe = regexp.MustCompile(`[\[\]()【】（）<>《》：:]`)

var genericPartRe = regexp.MustCompile(`(?i)^(?:cd|disc|disk|dvd|part|pt)?\s*0*([1-9]\d?)$`)

var genericNameRe = regexp.MustCompile(`(?i)^(movie|video|film|影片|电影|合集|全集|未命名)$`)

var splitDiscRe = regexp.MustCompile(`(?i)(?:\b(?:cd|disc|disk)\s*0*[12]\b|上集|下集)`)

var trailingPartRe = regexp.MustCompile(`(?i)^(.+?)\s*([1-9]\d?)$`)

var cnPartRe = regexp.MustCompile(`第([一二三四五六七八九十\d]{1,3})部`)

func looksLikeCollectionTitle(title string) bool {
	name, _ := parseTitle(title)
	if name == "" {
		name = title
	}
	return collectionTitleRe.MatchString(name) || collectionTitleRe.MatchString(title)
}

func seriesBaseFromTitle(title string) string {
	name, _ := parseTitle(title)
	if name == "" {
		name = stripYear(title)
	}
	name = stripReleaseTags(name)
	name = collectionTitleRe.ReplaceAllString(name, "")
	name = strings.TrimSpace(name)
	name = strings.Trim(name, "-_. ")
	return name
}

func stripReleaseTags(s string) string {
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.ReplaceAll(s, ".", " ")
	s = punctRe.ReplaceAllString(s, " ")
	s = siteJunkRe.ReplaceAllString(s, " ")
	s = leftoverSiteRe.ReplaceAllString(s, " ")
	s = releaseTagRe.ReplaceAllString(s, " ")
	s = zhReleaseTagRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func stripExtName(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

func looksGenericVideoName(cleaned string) bool {
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return true
	}
	if genericPartRe.MatchString(cleaned) {
		return true
	}
	return genericNameRe.MatchString(cleaned)
}

func parsePartNumber(s string) int {
	if m := genericPartRe.FindStringSubmatch(strings.TrimSpace(s)); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	if m := cnPartRe.FindStringSubmatch(s); m != nil && !strings.Contains(s, "部全") {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
		if n, ok := parseChinese(m[1]); ok {
			return n
		}
	}
	return 0
}

// inferMovieTitle 从单个视频文件名/路径推断「这一部电影」的标题。
// 合集包里常见 1.mp4、钢铁侠2.mkv、子目录 3/xxx.mp4。
func inferMovieTitle(fileName, filePath, collectionTitle string) string {
	series := seriesBaseFromTitle(collectionTitle)
	cleaned := stripReleaseTags(stripExtName(fileName))

	if splitDiscRe.MatchString(cleaned) && parsePartNumber(cleaned) == 0 {
		if series != "" {
			return series
		}
		return cleaned
	}

	if n := parsePartNumber(cleaned); n > 0 && looksGenericVideoName(cleaned) {
		if series != "" {
			return series + strconv.Itoa(n)
		}
	}

	if filePath != "" {
		parent := path.Base(path.Dir(filePath))
		parentClean := stripReleaseTags(parent)
		if n := parsePartNumber(parentClean); n > 0 && looksGenericVideoName(cleaned) {
			if series != "" {
				return series + strconv.Itoa(n)
			}
		}
	}

	if cleaned != "" && !looksLikeCollectionTitle(cleaned) && !looksGenericVideoName(cleaned) {
		return cleaned
	}

	if n := parsePartNumber(cleaned); n > 0 && series != "" {
		return series + strconv.Itoa(n)
	}

	if m := trailingPartRe.FindStringSubmatch(cleaned); m != nil && series != "" {
		base := strings.TrimSpace(m[1])
		if base == series || strings.Contains(series, base) || strings.Contains(base, series) {
			return series + m[2]
		}
	}

	if series != "" && (cleaned == "" || looksLikeCollectionTitle(cleaned) || looksGenericVideoName(cleaned)) {
		return series
	}
	if cleaned != "" {
		return cleaned
	}
	if series != "" {
		return series
	}
	return stripExtName(fileName)
}

func magnetLooksLikeMoviePart(magnetName, pageTitle string) bool {
	cleaned := stripReleaseTags(stripExtName(magnetName))
	if cleaned == "" || looksLikeCollectionTitle(cleaned) {
		return false
	}
	if looksGenericVideoName(cleaned) {
		return parsePartNumber(cleaned) > 0
	}
	if parsePartNumber(cleaned) > 0 {
		return true
	}
	if m := trailingPartRe.FindStringSubmatch(cleaned); m != nil {
		return true
	}
	return false
}
