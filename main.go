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
				fileList := GetDownloadFileListById(v.Bvid)
				if len(fileList) > 1 {
					// 剧集
					for _, f := range fileList {
						f.Index = len(ret)
						f.SubDir = path.Join(v.Title, f.SubDir)
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
				fileName := fmt.Sprintf("%d_%s", bfi.Index+1, bfi.Name)
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
		bfis = GetDownloadFileListById(id)
	}
	if len(bfis) == 0 {
		fmt.Println("no files found")
	} else {
		fmt.Printf("all %d files will be download.\n", len(bfis))
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
	DownloadFiles(bfis, saveDir)
}
