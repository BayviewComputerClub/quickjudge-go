package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"github.com/gin-gonic/gin"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

var (
	router  *gin.Engine
	postNum int64
)

type Request struct {
	ProblemID string `json:"problemID"`
	UserID    string `json:"userID"`
	InputCode string `json:"inputCode"`
	Lang      string `json:"lang"`
	Input     string `json:"input"`
	Output    string `json:"output"`
	Timelimit int `json:"timelimit"`
}

type Return struct {
	Accepted       bool   `json:"accepted"`
	Time           int    `json:"time"`
	IsCompileError bool   `json:"isCompileError"`
	ErrorContent   string `json:"errorContent"`
	IsTLE          bool   `json:"isTLE"`
	Score          int    `json:"score"`
	ErrorAt        int    `json:"errorAt"`
	OtherError     bool   `json:"otherError"`
}

func main() {
	log.Println("Starting BayviewJudge-Grader (Go)")

	// Init web-server
	router = gin.Default()

	router.Use(gin.Logger())
	router.Use(gin.Recovery())

	router.POST("/v1/judge-submission", func(c *gin.Context) {
		var req Request
		err := c.BindJSON(&req)
		if err != nil {
			log.Println(err.Error())
			return
		}
		log.Println("Received Request " + req.InputCode)
		try(req, c)
	})

	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(3000),
		Handler: router,
	}

	// start web-server in goroutine
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// listen for sigint to shutdown gracefully
	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("Shutting down BSSPC Grader...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server shutdown: ", err)
	}

	log.Println("BSSPC Grader has stopped.")
}

func try(req Request, c *gin.Context) {
	switch req.Lang {
	case "c++":

		ns, err := base64.StdEncoding.WithPadding('=').DecodeString(req.InputCode)
		if err != nil {
			log.Println(err.Error())
			return
		}

		stamp := strconv.FormatInt(time.Now().Unix(), 10)

		defer os.Remove("./" + stamp + ".cpp")
		err = ioutil.WriteFile("./" + stamp + ".cpp", []byte(ns), 0644)
		if err != nil {
			log.Println(err.Error())
			return
		}

		// compile the program

		cmd := exec.Command("g++", "./" + stamp + ".cpp", "-o", "./main")
		runTest(req, c, cmd, "./" + stamp, "")

	case "java":

		ns, err := base64.StdEncoding.WithPadding('=').DecodeString(req.InputCode)
		if err != nil {
			log.Println(err.Error())
			return
		}

		stamp := strconv.FormatInt(time.Now().Unix(), 10)

		defer os.Remove("./C" + stamp + ".java")
		ns = []byte(strings.ReplaceAll(string(ns), "class Main", "class C" + stamp))
		err = ioutil.WriteFile("./C" + stamp + ".java", []byte(ns), 0644)
		if err != nil {
			log.Println(err.Error())
			return
		}

		// compile the program

		cmd := exec.Command("javac", "C" + stamp + ".java")
		runTest(req, c, cmd, "java", "C" + stamp)

	case "python":

		ns, err := base64.StdEncoding.WithPadding('=').DecodeString(req.InputCode)
		if err != nil {
			log.Println(err.Error())
			return
		}

		stamp := strconv.FormatInt(time.Now().Unix(), 10)

		defer os.Remove("./" + stamp + ".py")
		err = ioutil.WriteFile("./" + stamp +".py", []byte(ns), 0644)
		if err != nil {
			log.Println(err.Error())
			return
		}

		// compile the program

		cmd := exec.Command("ls")
		runTest(req, c, cmd, "python3", stamp + ".py")

	}
}

func runTest(req Request, c *gin.Context, cmd *exec.Cmd, runCMD string, runArg string) {
	err := cmd.Run()
	if err != nil {
		fmt.Println("Compile Error")
		c.JSON(200, Return{ // COMPILE ERROR
			Accepted:       false,
			Time:           0,
			IsCompileError: true,
			ErrorContent:   err.Error(),
			IsTLE:          false,
			Score:          0,
			ErrorAt:        0,
			OtherError:     false,
		})
		return
	}

	// run the program

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(req.Timelimit)*time.Second)
	defer cancel()

	cmd = exec.CommandContext(ctx, runCMD, runArg)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Println("Runtime Error")
		c.JSON(200, Return{ // RUNTIME ERROR
			Accepted:       false,
			Time:           0,
			IsCompileError: false,
			ErrorContent:   err.Error(),
			IsTLE:          false,
			Score:          0,
			ErrorAt:        0,
			OtherError:     true,
		})
		return
	}
	defer stdin.Close()
	err = cmd.Start()

	if err != nil {
		log.Println(err.Error())
		return
	}

	l, err := io.WriteString(stdin, req.Input)

	if err != nil {
		log.Println(err.Error() + " " + strconv.Itoa(l))
		return
	}

	out, err := ioutil.ReadAll(cmd.Stdin)
	out, err := cmd.Output()
	//TODO FIX INPUT PIPE

	if ctx.Err() == context.DeadlineExceeded { // TLE
		cmd.Process.Kill()
		fmt.Println("TLE")
		c.JSON(200, Return{
			Accepted:       false,
			Time:           2,
			IsCompileError: false,
			ErrorContent:   "",
			IsTLE:          true,
			Score:          0,
			ErrorAt:        0,
			OtherError:     false,
		})
		return
	} else if ctx.Err() != nil {
		fmt.Println("Runtime Error")
		c.JSON(200, Return{ // RUNTIME ERROR
			Accepted:       false,
			Time:           0,
			IsCompileError: false,
			ErrorContent:   err.Error(),
			IsTLE:          false,
			Score:          0,
			ErrorAt:        0,
			OtherError:     true,
		})
		return
	}

	// judge the output
	s := string(out)
	o := req.Output

	log.Println("Judging...")

	log.Println(s)

	s = strings.ReplaceAll(s, "\r", "")
	o = strings.ReplaceAll(o, "\r", "")
	s = strings.ReplaceAll(s, " ", "")
	o = strings.ReplaceAll(o, " ", "")
	s = strings.ReplaceAll(s, "\n", "")
	o = strings.ReplaceAll(o, "\n", "")

	log.Println(s + "\n" + o)

	if s != o {
		fmt.Println("WA")
		c.JSON(200, Return{ // WA
			Accepted:       false,
			Time:           0,
			IsCompileError: false,
			ErrorContent:   "",
			IsTLE:          false,
			Score:          0,
			ErrorAt:        0,
			OtherError:     true,
		})
		return
	}

	//arr := strings.Split(s, "\n")
	//arr2 := strings.Split(o, "\n")

	/*
	for i, ele := range arr {
		ref := arr2[i]
//		ele = strings.Replace(ele, "\n", "", -1)
//		ref = strings.Replace(ref, "\n", "", -1)

		ele = strings.ReplaceAll(ele, " ", "")
		ref = strings.ReplaceAll(ref, " ", "")

		//ele = strings.TrimRight(ele, " ")
		//ref = strings.TrimRight(ele, " ")

		if ele != ref {
			fmt.Println("WA")
			c.JSON(200, Return{ // WA
				Accepted:       false,
				Time:           0,
				IsCompileError: false,
				ErrorContent:   "",
				IsTLE:          false,
				Score:          0,
				ErrorAt:        0,
				OtherError:     true,
			})
			return
		}
	}*/

	fmt.Println("AC")
	c.JSON(200, Return{ // AC
		Accepted:       true,
		Time:           0,
		IsCompileError: false,
		ErrorContent:   "",
		IsTLE:          false,
		Score:          0,
		ErrorAt:        0,
		OtherError:     true,
	})
}
