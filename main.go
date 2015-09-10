package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/astaxie/beego/logs"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

type Config struct {
	From               string
	To                 string
	Cmd                string
	ParamsFmt          string
	LanunchPath        string
	ControlXmlPath     string
	AndroidManifestXml string
}

const configName = "h5release.json"

var log = logs.NewLogger(1024)
var config Config

func main() {

	log.SetLogger("console", "")
	log.SetLevel(logs.LevelInfo)
	log.EnableFuncCallDepth(true)
	log.Debug("args:%#v", flag.Args())

	log.Info("main run")
	bs, err := ioutil.ReadFile(configName)
	if err != nil {
		log.Error(err.Error())
		config.From = "www"
		config.To = `assets/apps/%HBUILDERID%/www`
		config.Cmd = `c:\Users\egood\AppData\Roaming\npm\uglifyjs.cmd`
		config.ParamsFmt = `%FileName% -o %FileBaseName%.min.js`
		config.ControlXmlPath = `assets\data\control.xml`
		config.AndroidManifestXml = `AndroidManifest.xml`
		bs, _ = json.MarshalIndent(&config, "", " ")
		ioutil.WriteFile(configName, bs, 0666)
		return
	}
	err = json.Unmarshal(bs, &config)
	if err != nil {
		log.Error(err.Error())
		return
	}

	nProc := runtime.NumCPU() * 2
	wg := sync.WaitGroup{}
	chnProc := make(chan *exec.Cmd, nProc)
	for i := 0; i < nProc; i++ {
		go func() {
			for c := range chnProc {
				c.Start()
				c.Wait()
				wg.Done()
			}
		}()
	}
	manifestMap, err := PrepareProc()
	if err != nil {
		log.Error("PrepareProc err:%#v", err)
		return
	}
	err = ProcVersion(manifestMap)

	err = ProcLaunchPageScript(config.LanunchPath)
	if err != nil {
		log.Error("ProcLaunchPageScript err:%#v", err)
		return
	}
	config.From = strings.Replace(config.From, "/", string(os.PathSeparator), -1)
	config.To = strings.Replace(config.To, "/", string(os.PathSeparator), -1)
	log.Info("%#v", config)
	RunPath(config.From, config.To, func(from, to string, fi os.FileInfo) (err error) {
		name := fi.Name()
		os.MkdirAll(to, 0666)
		switch {
		case fi.Name() == "manifest.json":
			dstName := to + string(os.PathSeparator) + fi.Name()
			bs, err := json.Marshal(manifestMap)
			if err != nil {
				log.Error(err.Error())
				break
			}
			err = ioutil.WriteFile(dstName, bs, 0666)
			if err != nil {
				log.Error(err.Error())
				break
			}
		case strings.HasSuffix(name, "min.js"):
		case strings.HasSuffix(name, ".js"):
			FileName := from + string(os.PathSeparator) + fi.Name()
			log.Info("minijs:%s", FileName)
			FileBaseName := to + string(os.PathSeparator) + strings.Split(name, ".js")[0]
			paramsfmt := config.ParamsFmt
			paramsfmt = strings.Replace(paramsfmt, `%FileName%`, FileName, -1)
			paramsfmt = strings.Replace(paramsfmt, `%FileBaseName%`, FileBaseName, -1)

			log.Debug("%s", paramsfmt)
			args := strings.Split(paramsfmt, " ")
			log.Debug("args:%#v", args)

			c := exec.Command(config.Cmd, args...)
			chnProc <- c
			wg.Add(1)
		case !fi.IsDir():
			srcName := from + string(os.PathSeparator) + fi.Name()
			dstName := to + string(os.PathSeparator) + fi.Name()
			log.Info("copyfile from:%s to:%s", srcName, dstName)
			err = CopyFile(dstName, srcName)
			if err != nil {
				log.Emergency("CopyFile err :%#v", err)
			}

		}

		return
	})
	wg.Wait()
}
func CopyFile(dst, src string) (err error) {
	bs, err := ioutil.ReadFile(src)
	if err != nil {
		log.Error("ReadFile err:%#v", err)
		return
	}
	err = ioutil.WriteFile(dst, bs, 0666)
	return
}

func RunPath(from, to string, each func(string, string, os.FileInfo) error) (err error) {
	l, err := ioutil.ReadDir(from)
	if err != nil {
		log.Error("ioutil.ReadDir(from) err:%s", err)
		return
	}
	for _, f := range l {
		err = each(from, to, f)
		if err != nil {
			log.Error("each err:%#v", err)
			return
		}
		newfrom := from + string(os.PathSeparator) + f.Name()
		newto := to + string(os.PathSeparator) + f.Name()
		log.Debug(newfrom)
		if f.IsDir() {
			err = RunPath(newfrom, newto, each)
			if err != nil {
				return
			}

		}
	}
	return
}

func PrepareProc() (m map[string]interface{}, err error) {
	f, err := os.Open(config.From + string(os.PathSeparator) + "manifest.json")
	if err != nil {
		log.Info("no hbuilder 'manifest.json' file")
		return
	}
	defer f.Close()

	jr := NewJsonCommentReader(f)
	err = json.NewDecoder(jr).Decode(&m)
	if err != nil {
		fmt.Println()
		log.Error("parse 'manifest.json' file err:%#v", err)
		log.Error("%#v", jr)
		return
	}
	log.Debug("json:%#v", m)
	config.To = strings.Replace(config.To, "%HBUILDERID%", m["id"].(string), -1)
	config.LanunchPath = config.From + string(os.PathSeparator) + m["launch_path"].(string)
	return
}

type JsonCommentReader struct {
	r            io.Reader
	prv          byte
	quotes       int  //绰号个数
	vilidchar    bool //有效json字符
	slashcomment bool //双斜杠注释
	blockcomment bool //块注释
}

func (this *JsonCommentReader) Read(p []byte) (n int, err error) {
	l, err := this.r.Read(p)
	if err != nil {
		return
	}
	i := 0

	for ; i < l; i++ {
		b := p[i]
		this.vilidchar = false
		switch b {
		case '"':
			this.vilidchar = true
			if this.quotes == 0 {
				this.quotes++ //first quotes see next quotes
				break
			}
			if this.prv == '\\' {
				break
			} else {
				this.quotes--
			}
		case '/':
			if this.quotes > 0 {
				this.vilidchar = true
				break
			}
			if this.prv == '/' {
				if !this.blockcomment {
					this.slashcomment = true
				}
			}
			if this.blockcomment {
				if this.prv == '*' {
					this.blockcomment = false
				}
			}
		case '\n':
			if this.quotes > 0 {
				this.vilidchar = true
				break
			}
			if this.slashcomment {
				this.vilidchar = true
				this.blockcomment = false
			}
		case '*':
			if this.quotes > 0 {
				this.vilidchar = true
				break
			}
			if this.prv == '/' {
				this.blockcomment = true
			}

		case '{', '}', '[', ']', ',', ':':
			if this.blockcomment ||
				this.slashcomment {
				this.vilidchar = false
				break
			}
			this.vilidchar = true
		default:
			if this.blockcomment ||
				this.slashcomment {
				this.vilidchar = false
				break
			}
			this.vilidchar = true
		}
		this.prv = b

		if this.vilidchar {
			p[n] = b
			n++
		}
	}
	return n, err
}
func NewJsonCommentReader(r io.Reader) *JsonCommentReader {
	obj := &JsonCommentReader{
		r: r,
	}
	return obj
}

func ProcLaunchPageScript(fileName string) (err error) {
	f, err := os.Open(fileName)
	if err != nil {
		return
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		log.Error("%#v", err)
	}

	doc.Find("script[src]").Each(func(i int, s *goquery.Selection) {
		src, ok := s.Attr("src")
		if ok {
			if !strings.HasSuffix(src, "min.js") {
				err = fmt.Errorf("%s not use min", src)
				log.Error(err.Error())
				return
			}
		}
	})
	return
}

func ProcVersion(manifestMap map[string]interface{}) (err error) {
	err = ProcControlXml(manifestMap)
	if err != nil {
		return
	}
	err = ProcAppManifestXml(manifestMap)
	if err != nil {
		return
	}
	return
}
func ProcControlXml(manifestMap map[string]interface{}) (err error) {
	bs, err := ioutil.ReadFile(config.ControlXmlPath)
	if err != nil {
		log.Error("%v", err)
		return
	}
	versionM := manifestMap["version"].(map[string]interface{})
	log.Debug("version:%s code:%s", versionM["name"], versionM["code"])

	r, err := regexp.Compile(fmt.Sprintf(`appid="%s"\s+appver="[\w\.]+"`, manifestMap["id"]))
	if err != nil {
		log.Error("%v", err)
		return
	}

	s := r.ReplaceAllString(string(bs), fmt.Sprintf(`appid="%s" appver="%s"`, manifestMap["id"], versionM["name"]))
	log.Debug("after replace:%s", s)
	err = ioutil.WriteFile(config.ControlXmlPath, []byte(s), 0666)
	return
}
func ProcAppManifestXml(manifestMap map[string]interface{}) (err error) {
	bs, err := ioutil.ReadFile(config.AndroidManifestXml)
	if err != nil {
		log.Error("%v", err)
		return
	}
	versionM := manifestMap["version"].(map[string]interface{})
	log.Debug("version:%s code:%s", versionM["name"], versionM["code"])

	r, err := regexp.Compile(`android:versionCode\s*=\s*"[\w+\.]+"\s*android:versionName\s*=\s*"[\w\.]+"`)
	if err != nil {
		log.Error("%v", err)
		return
	}

	s := r.ReplaceAllString(string(bs), fmt.Sprintf(`android:versionCode="%s" android:versionName="%s"`, versionM["code"], versionM["name"]))
	log.Debug("after replace:%s", s)
	err = ioutil.WriteFile(config.AndroidManifestXml, []byte(s), 0666)
	return
}
