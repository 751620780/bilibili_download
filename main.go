package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// 内存数据写入文件
func WriteMemFileToDisk(fileName string, data []byte) error {
	dirName := path.Dir(fileName)
	if !DirExists(dirName) {
		os.MkdirAll(dirName, 0777)
	}
	return ioutil.WriteFile(fileName, data, 0666)
}

// 获得本进程可执行程序的完整路径
func GetExecutableFullPath() string {
	exePath, err := os.Executable()
	if err != nil {
		return ""
	}
	exePath, err = filepath.Abs(exePath)
	if err != nil {
		return ""
	}
	return exePath
}

// 判断文件是否存在
func FileExists(path string) bool {
	fi, err := os.Stat(path)
	if err == nil {
		return !fi.IsDir()
	}
	return false
}

func RunCmd(command string) (exitCode int, stdOutput string, errOutput string, err error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	var ShellToUse string

	switch runtime.GOOS {
	case "linux":
		ShellToUse = "bash"
	case "windows":
		ShellToUse = "powershell"
	default:
		ShellToUse = "bash"
	}
	cmd := exec.Command(ShellToUse, "-c", command)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	stdOutput = stdout.String()
	errOutput = stderr.String()
	exitCode = cmd.ProcessState.ExitCode()
	return
}

func DirExists(path string) bool {
	fi, err := os.Stat(path)
	if err == nil {
		return fi.IsDir()
	}
	return false
}

type BiliFileInfo struct {
	Name   string // 文件标题
	Url    string // 下载视频的url
	SubDir string // 保存的子文件夹
	Index  int    // 下载文件的编号
}

func DoGet(client *http.Client, url string) ([]byte, error) {

	req, _ := http.NewRequest("GET", url, nil)
	// 增加请求头，绕过B站的爬虫检测
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/106.0.0.0 Safari/537.36 Edg/106.0.1370.52")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf(`DoGet failed when client.Get, url=%s, err: %s`, url, err.Error())
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf(`Post failed when ioutil.ReadAll, url=%s, err: %s`, url, err.Error())
		return nil, err
	}
	return respBody, nil
}

func GetDownloadFileListByUser(bilibiliUserId string) []*BiliFileInfo {
	// https://api.bilibili.com/x/space/arc/search?mid=40018594&ps=30&tid=0&pn=4

	type BilibiliUserVideoListInfo struct {
		Code    int    `json:"code"`    // 请求状态码
		Message string `json:"message"` // 信息
		TTL     int    `json:"ttl"`     // 请求耗时
		Data    struct {
			List struct {
				Tlist interface{} `json:"tlist"`
				Vlist []struct {
					Comment        int    `json:"comment"`
					Typeid         int    `json:"typeid"`
					Play           int    `json:"play"`
					Pic            string `json:"pic"`
					Subtitle       string `json:"subtitle"`
					Description    string `json:"description"`
					Copyright      string `json:"copyright"`
					Title          string `json:"title"`
					Review         int    `json:"review"`
					Author         string `json:"author"`
					Mid            int    `json:"mid"`
					Created        int    `json:"created"`
					Length         string `json:"length"`
					VideoReview    int    `json:"video_review"`
					Aid            int    `json:"aid"`
					Bvid           string `json:"bvid"`
					HideClick      bool   `json:"hide_click"`
					IsPay          int    `json:"is_pay"`
					IsUnionVideo   int    `json:"is_union_video"`
					IsSteinsGate   int    `json:"is_steins_gate"`
					IsLivePlayback int    `json:"is_live_playback"`
				} `json:"vlist"` // 本页的视频列表
			} `json:"list"`
			Page struct {
				Pn    int `json:"pn"`    // 第几页
				Ps    int `json:"ps"`    // 每页大小
				Count int `json:"count"` // 总数，并非本页总数
			} `json:"page"`
		} `json:"data"`
	}
	ret := make([]*BiliFileInfo, 0, 1000)
	client := http.Client{Timeout: time.Second * 5}
	// i 从1开始
	for i := 1; i < math.MaxInt32; i++ {
		responseData, err := DoGet(&client, fmt.Sprintf("https://api.bilibili.com/x/space/arc/search?mid=%s&ps=30&tid=0&pn=%d", bilibiliUserId, i))
		if err != nil {
			break
		} else {
			buvli := BilibiliUserVideoListInfo{}
			err = json.Unmarshal(responseData, &buvli)
			if err != nil {
				fmt.Printf("json.Unmarshal failed, responseData=%s, err:%s", string(responseData), err.Error())
				break
			}
			if buvli.Code != 0 {
				fmt.Printf("code=%d, responseData:%s", buvli.Code, string(responseData))
				break
			}
			if len(buvli.Data.List.Vlist) == 0 {
				break
			}

			for _, v := range buvli.Data.List.Vlist {
				// 判断这个视频是不是连续剧，如果是连续剧，下载到子文件夹下
				title, fileList := GetDownloadFileListByIdNew(v.Bvid)
				if len(fileList) > 1 {
					// 剧集
					for _, f := range fileList {
						f.Index = len(ret)
						f.SubDir = path.Join(title, f.SubDir)
						ret = append(ret, f)
					}
				} else {
					// 单集
					ret = append(ret, &BiliFileInfo{Name: v.Title, Url: fmt.Sprintf("https://www.bilibili.com/video/%s", v.Bvid), Index: len(ret), SubDir: "."})
				}
			}
		}
	}

	return ret
}

// 给定B站视频的bv号自动判断剧集还是单集，如果是剧集则获得所有剧集信息，如果是单集，返回单集信息
func GetDownloadFileListByIdNew(bvid string) (string, []*BiliFileInfo) {
	// step1:通过接口查询bvid关联的剧集的列表
	// https://api.bilibili.com/x/web-interface/view?bvid=BV1fT411M7Zt
	// 存在三种情况：
	// 1.一个视频BV号下多个视频，url形式是：BV1ke4y1n7a9?p=4，最常见的剧集形式
	// 2.一个视频BV号下仅有一个视频，但是这个视频关联到一个剧集中，此剧集的多个视频分别使用一个bv号
	// 3.这个BV号仅有一个视频且不关联任何视频

	// bvid关联的视频信息
	type BvidSeriesInfo struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		TTL     int    `json:"ttl"`
		Data    struct {
			Bvid   string `json:"bvid"`
			Aid    int    `json:"aid"`
			Videos int    `json:"videos"` // 值大于1，是第一种情况，使用Pages中的数据，表示剧集下的视频数量
			Title  string `json:"title"`  // 情况1时，剧集的标题
			Owner  struct {
				Mid  int    `json:"mid"`
				Name string `json:"name"`
				Face string `json:"face"`
			} `json:"owner"` // 视频拥有者
			Pages []struct {
				Cid       int    `json:"cid"`
				Page      int    `json:"page"` // 视频在剧集中的编号
				From      string `json:"from"`
				Part      string `json:"part"` // 视频名字
				Duration  int    `json:"duration"`
				Vid       string `json:"vid"`
				Weblink   string `json:"weblink"`
				Dimension struct {
					Width  int `json:"width"`
					Height int `json:"height"`
					Rotate int `json:"rotate"`
				} `json:"dimension"`
				FirstFrame string `json:"first_frame"`
			} `json:"pages"` // 情况1时的视频列表
			UgcSeason struct {
				ID       int    `json:"id"`
				Title    string `json:"title"` // 情况2时，剧集的标题
				Sections []struct {
					SeasonID int `json:"season_id"`
					ID       int `json:"id"`
					Type     int `json:"type"`
					Episodes []struct {
						SeasonID  int    `json:"season_id"`
						SectionID int    `json:"section_id"`
						ID        int    `json:"id"`
						Aid       int    `json:"aid"`
						Cid       int    `json:"cid"`
						Title     string `json:"title"` // 视频名称
						Bvid      string `json:"bvid"`  // 每一个剧集的bvid
					} `json:"episodes"` // 情况二时，剧集列表，索引是视频编号
				} `json:"sections"` // 情况二时可能属于不同的分组，这里使用第一组即可
				EpCount int `json:"ep_count"` // 情况二时，剧集的数量
			} `json:"ugc_season"`
		} `json:"data"`
	}
	title := ""
	ret := make([]*BiliFileInfo, 0, 1000)
	client := http.Client{Timeout: time.Second * 5}
	responseData, err := DoGet(&client, fmt.Sprintf("https://api.bilibili.com/x/web-interface/view?bvid=%s", bvid))
	if err != nil {
		return title, ret
	} else {
		bsi := BvidSeriesInfo{}
		err = json.Unmarshal(responseData, &bsi)
		if err != nil {
			fmt.Printf("json.Unmarshal failed, responseData=%s, err:%s", string(responseData), err.Error())
			return title, ret
		}
		if bsi.Code != 0 {
			fmt.Printf("code=%d, responseData:%s", bsi.Code, string(responseData))
			return title, ret
		}
		if len(bsi.Data.Pages) > 1 {
			// 情况1
			title = bsi.Data.Title
			for _, v := range bsi.Data.Pages {
				ret = append(ret, &BiliFileInfo{Name: fmt.Sprintf("%d_%s", v.Page, v.Part), Url: fmt.Sprintf("https://www.bilibili.com/video/%s?p=%d", bvid, v.Page), Index: len(ret), SubDir: "."})
			}
		} else if len(bsi.Data.Pages) == 1 && bsi.Data.UgcSeason.EpCount == 0 {
			// 情况3
			v := bsi.Data.Pages[0]
			ret = append(ret, &BiliFileInfo{Name: v.Part, Url: fmt.Sprintf("https://www.bilibili.com/video/%s", bvid), Index: len(ret), SubDir: "."})
		} else if bsi.Data.UgcSeason.EpCount > 0 {
			// 情况2
			title = bsi.Data.UgcSeason.Title
			for i, v := range bsi.Data.UgcSeason.Sections[0].Episodes {
				ret = append(ret, &BiliFileInfo{Name: fmt.Sprintf("%d_%s", i+1, v.Title), Url: fmt.Sprintf("https://www.bilibili.com/video/%s", v.Bvid), Index: len(ret), SubDir: "."})
			}
		}
	}

	return title, ret
}

func GetDownloadFileListById(bilibiliSeriesVideoId string) []*BiliFileInfo {
	// https://api.bilibili.com/x/player/pagelist?bvid=BV1QB4y1F722&jsonp=jsonp

	type BilibiliSeriesVideoListInfo struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		TTL     int    `json:"ttl"`
		Data    []struct {
			Cid      int    `json:"cid"`
			Page     int    `json:"page"` // 视频集数编号
			From     string `json:"from"`
			Part     string `json:"part"` // 视频标题
			Duration int    `json:"duration"`
			Vid      string `json:"vid"`
			Weblink  string `json:"weblink"`
		} `json:"data"`
	}
	ret := make([]*BiliFileInfo, 0, 1000)
	request := http.Client{Timeout: time.Second * 5}
	responseData, err := DoGet(&request, fmt.Sprintf("https://api.bilibili.com/x/player/pagelist?bvid=%s&jsonp=jsonp", bilibiliSeriesVideoId))
	if err != nil {
		return ret
	} else {
		bsvli := BilibiliSeriesVideoListInfo{}
		err = json.Unmarshal(responseData, &bsvli)
		if err != nil {
			fmt.Printf("json.Unmarshal failed, responseData=%s, err:%s", string(responseData), err.Error())
			return ret
		}
		if bsvli.Code != 0 {
			fmt.Printf("code=%d, responseData:%s", bsvli.Code, string(responseData))
			return ret
		}
		for _, v := range bsvli.Data {
			ret = append(ret, &BiliFileInfo{Name: v.Part, Url: fmt.Sprintf("https://www.bilibili.com/video/%s?p=%d", bilibiliSeriesVideoId, v.Page), Index: len(ret), SubDir: "."})
		}
	}

	return ret
}

func DownloadFiles(bfis []*BiliFileInfo, saveDir string) {
	var wg sync.WaitGroup
	c := make(chan *BiliFileInfo, 1000)
	downloadDir, _ := filepath.Abs(saveDir)
	for _, bfi := range bfis {
		c <- bfi
	}
	downloadCount := runtime.NumCPU() / 2
	if downloadCount < 12 {
		downloadCount = 12
	}
	for i := 0; i < downloadCount; i++ {
		c <- nil
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				bfi := <-c
				if bfi == nil {
					return
				}
				fileName := fmt.Sprintf(bfi.Name)
				realDownloadDir := path.Join(downloadDir, bfi.SubDir)
				if !DirExists(realDownloadDir) {
					os.MkdirAll(realDownloadDir, 0666)
				}
				cmdLine := fmt.Sprintf(`you-get -o "%s" -O "%s" --skip-existing-file-size-check %s --no-caption --debug`, realDownloadDir, fileName, bfi.Url)
				exitCode, stdOutput, errOutput, err := RunCmd(cmdLine)
				if exitCode != 0 {
					fmt.Printf("download failed: index=%d name=%s url=%s output=%s error_output=%s\n cmd_line=%s\n", bfi.Index, bfi.Name, bfi.Url, stdOutput, errOutput, cmdLine)
				} else if err != nil {
					fmt.Printf("download fail: index=%d name=%s url=%s output=%s error_output=%s err:%s\n cmd_line=%s\n", bfi.Index, bfi.Name, bfi.Url, stdOutput, errOutput, err.Error(), cmdLine)
				} else {
					fmt.Printf("download succ: index=%d name=\"%s\"\turl=\"%s\"\n", bfi.Index, bfi.Name, bfi.Url)
				}
			}
		}()
	}
	wg.Wait()
}

func PrintUsage() {
	fmt.Printf(`
	USAGE：
		this_file    user/series     user_id/series_id     save_dir
		
		下载哔哩哔哩的视频，user表示用户的全部视频，series表示视频集的所有视频。
	
	`)
	time.Sleep(time.Second)
}

func main() {
	// type=user/series id savedir
	var bfis []*BiliFileInfo = nil
	if len(os.Args) != 4 {
		PrintUsage()
		os.Exit(-1)
	}
	downloadType := strings.ToLower(os.Args[1])
	id := os.Args[2]
	saveDir := os.Args[3]
	if !DirExists(saveDir) {
		err := os.MkdirAll(saveDir, 0666)
		if err != nil {
			fmt.Printf("os.MkdirAll(\"%s\", 0666) failed, err: %s", saveDir, err.Error())
			PrintUsage()
			os.Exit(-2)
		}
	}
	if downloadType == "user" {
		bfis = GetDownloadFileListByUser(id)
	} else if downloadType == "series" {
		_, bfis = GetDownloadFileListByIdNew(id)
	}
	// 去除重复文件
	bfisNew := make([]*BiliFileInfo, 0, 1000)
	urlMap := make(map[string]int, 1000)
	for _, bfi := range bfis {
		if _, ok := urlMap[bfi.Url]; !ok {
			bfi.Index = len(bfisNew)
			bfisNew = append(bfisNew, bfi)
			urlMap[bfi.Url] = 0
		}
	}
	bfis = bfisNew

	if len(bfis) == 0 {
		fmt.Println("没有文件需要下载")
	} else {
		fmt.Printf("一共有 %d 个文件将会被下载.\n", len(bfis))
	}
	exitCode, _, _, _ := RunCmd("you-get --version")
	if exitCode != 0 {
		fmt.Printf(`
下载失败！
您还没有安装you-get，请您按照如下步骤检查：
	1. 确认已经安装了python3
	2. 确认在python3安装了you-get模块(pip install you-get)
	3. 确认you-get是最新版(pip install --upgrade you-get)
	4. 确认将python3的Scripts目录添加到环境变量
	`)
		os.Exit(-1)
	}
	// 如果存在bat文件则不写入，否则创建并写入该文件
	exePath := GetExecutableFullPath()
	targetFile := path.Join(saveDir, "0.cmd")
	if !FileExists(targetFile) {
		fileDate := fmt.Sprintf("\"%s\"  %s  %s  \"%s\"", exePath, downloadType, id, saveDir)
		WriteMemFileToDisk(targetFile, []byte(fileDate))
	}
	DownloadFiles(bfis, saveDir)
}
