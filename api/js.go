package api

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/dop251/goja"
	"github.com/labstack/echo/v4"

	"sealdice-core/dice"
)

func jsExec(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return c.JSON(200, map[string]interface{}{
			"testMode": true,
		})
	}
	if !myDice.Config.JsEnable {
		resp := c.JSON(200, map[string]interface{}{
			"result": false,
			"err":    "js扩展支持已关闭",
		})
		return resp
	}

	v := struct {
		Value string `json:"value"`
	}{}
	err := c.Bind(&v)
	if err != nil {
		return c.String(430, err.Error())
	}

	source := "(function(exports, require, module) {" + v.Value + "\n})()"
	loop := myDice.ExtLoopManager.GetWebLoop()
	waitRun := make(chan int, 1)

	var ret goja.Value
	myDice.JsPrinter.RecordStart()
	loop.RunOnLoop(func(vm *goja.Runtime) {
		defer func() {
			// 防止崩掉进程
			if r := recover(); r != nil {
				// fmt.Println("xx", r.(goja.Exception))
				myDice.JsPrinter.Error(fmt.Sprintf("JS脚本报错: %v", r))
			}
			waitRun <- 1
		}()
		ret, err = vm.RunString(source)
	})
	<-waitRun
	outputs := myDice.JsPrinter.RecordEnd()

	var retFinal interface{}
	if ret != nil {
		retFinal = ret.Export()
	}

	var errText interface{}
	if err != nil {
		errText = err.Error()
	}

	resp := c.JSON(200, map[string]interface{}{
		"result":  true,
		"ret":     retFinal,
		"outputs": outputs,
		"err":     errText,
	})

	return resp
}

func jsGetRecord(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if !myDice.Config.JsEnable {
		resp := c.JSON(200, map[string]interface{}{
			"outputs": []string{},
		})
		return resp
	}

	outputs := myDice.JsPrinter.RecordEnd()
	resp := c.JSON(200, map[string]interface{}{
		"outputs": outputs,
	})
	return resp
}

func jsDelete(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return c.JSON(200, map[string]interface{}{
			"testMode": true,
		})
	}
	if !myDice.Config.JsEnable {
		resp := c.JSON(200, map[string]interface{}{
			"result": false,
			"err":    "js扩展支持已关闭",
		})
		return resp
	}

	v := struct {
		Filename string `json:"filename"`
	}{}
	err := c.Bind(&v)

	if err == nil && v.Filename != "" {
		for _, js := range myDice.JsScriptList {
			if js.Filename == v.Filename {
				dice.JsDelete(myDice, js)
				break
			}
		}
	}

	return c.JSON(http.StatusOK, nil)
}

func jsReload(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return c.JSON(200, map[string]interface{}{
			"testMode": true,
		})
	}
	// 尝试取锁，如果取不到，说明正在后台重载中
	// TODO:用户提示模式？
	locked := myDice.JsReloadLock.TryLock()
	if !locked {
		return c.NoContent(400)
	}
	defer myDice.JsReloadLock.Unlock()
	myDice.JsReload()
	return c.NoContent(200)
}

func jsUpload(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return c.JSON(200, map[string]interface{}{
			"testMode": true,
		})
	}

	// -----------
	// Read file
	// -----------

	// Source
	file, err := c.FormFile("file")
	if err != nil {
		return err
	}
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer func(src multipart.File) {
		_ = src.Close()
	}(src)

	fileName := sanitizeUploadedFilename(file.Filename)
	scriptsDir := filepath.Join(myDice.BaseConfig.DataDir, "scripts")
	dstPath := filepath.Join(scriptsDir, fileName)

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer func(dst *os.File) {
		_ = dst.Close()
	}(dst)

	// Copy
	if _, err = io.Copy(dst, src); err != nil {
		return err
	}

	if strings.EqualFold(filepath.Ext(fileName), ".zip") {
		destDir := filepath.Join(scriptsDir, "_"+fileName)
		if err = extractJSPluginPackage(dstPath, destDir); err != nil {
			_ = os.RemoveAll(destDir)
			_ = os.Remove(dstPath)
			return echo.NewHTTPError(http.StatusBadRequest, err.Error())
		}
	}

	return c.JSON(http.StatusOK, nil)
}

func sanitizeUploadedFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.ReplaceAll(filename, "/", "_")
	filename = strings.ReplaceAll(filename, "\\", "_")
	return filepath.Base(filename)
}

func normalizeZipEntryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "/")
	name = strings.TrimPrefix(name, "./")
	name = path.Clean("/" + name)
	name = strings.TrimPrefix(name, "/")
	if name == "" || name == "." {
		return "", nil
	}
	parts := strings.Split(name, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("zip entry contains invalid path")
		}
	}
	return name, nil
}

func openZipEntryToFile(file *zip.File, target string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer func(src io.ReadCloser) {
		_ = src.Close()
	}(src)

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	perm := file.Mode()
	if perm == 0 {
		perm = 0o644
	}
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func(dst *os.File) {
		_ = dst.Close()
	}(dst)
	_, err = io.Copy(dst, src)
	return err
}

func isWebUIUnderPath(filePath string, root string) bool {
	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absRoot = filepath.Clean(absRoot)
	if !strings.HasSuffix(absRoot, string(os.PathSeparator)) {
		absRoot += string(os.PathSeparator)
	}
	return strings.HasPrefix(absFile, absRoot)
}

func extractJSPluginPackage(zipPath string, destDir string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer func(reader *zip.ReadCloser) {
		_ = reader.Close()
	}(reader)

	type webuiEntry struct {
		file *zip.File
		rel  string
	}

	var scriptFiles []*zip.File
	var webuiFiles []webuiEntry

	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		name, err := normalizeZipEntryName(file.Name)
		if err != nil {
			return err
		}
		if name == "" {
			continue
		}

		parts := strings.Split(name, "/")
		webuiIndex := -1
		for idx, part := range parts {
			if strings.EqualFold(part, "webui") {
				webuiIndex = idx
				break
			}
		}
		if webuiIndex >= 0 {
			rel := path.Join(parts[webuiIndex+1:]...)
			if rel != "." && rel != "" {
				webuiFiles = append(webuiFiles, webuiEntry{
					file: file,
					rel:  rel,
				})
			}
			continue
		}

		if isPluginScriptFile(name) {
			scriptFiles = append(scriptFiles, file)
		}
	}

	if len(scriptFiles) == 0 {
		return errors.New("zip包中未找到插件脚本(.js/.ts)")
	}
	if len(scriptFiles) > 1 {
		return errors.New("zip包中存在多个插件脚本，仅支持一个入口文件")
	}
	if len(webuiFiles) == 0 {
		return errors.New("zip包中未找到webui目录")
	}

	_ = os.RemoveAll(destDir)
	if err = os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	scriptFile := scriptFiles[0]
	scriptName := sanitizeUploadedFilename(filepath.Base(scriptFile.Name))
	if scriptName == "" || !isPluginScriptFile(scriptName) {
		return errors.New("zip包中的脚本文件名无效")
	}
	scriptTarget := filepath.Join(destDir, scriptName)
	if err = openZipEntryToFile(scriptFile, scriptTarget); err != nil {
		return err
	}

	webuiRoot := filepath.Join(destDir, "webui")
	for _, item := range webuiFiles {
		target := filepath.Join(webuiRoot, filepath.FromSlash(item.rel))
		if !isWebUIUnderPath(target, webuiRoot) {
			return errors.New("webui目录存在非法路径")
		}
		if err = openZipEntryToFile(item.file, target); err != nil {
			return err
		}
	}

	if _, err = os.Stat(filepath.Join(webuiRoot, "index.html")); errors.Is(err, fs.ErrNotExist) {
		return errors.New("webui目录缺少index.html")
	}
	return err
}

func isPluginScriptFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".js" || ext == ".ts"
}

func jsList(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if !myDice.Config.JsEnable {
		resp := c.JSON(200, []*dice.JsScriptInfo{})
		return resp
	}

	type script struct {
		dice.JsScriptInfo
		BuiltinUpdated bool   `json:"builtinUpdated"`
		WebUIPath      string `json:"webUIPath,omitempty"`
	}
	scripts := make([]*script, 0, len(myDice.JsScriptList))
	for _, info := range myDice.JsScriptList {
		temp := script{
			JsScriptInfo:   *info,
			BuiltinUpdated: info.Builtin && !myDice.JsBuiltinDigestSet[info.Digest],
			WebUIPath:      getJsScriptWebUIPathByScriptInfo(info),
		}
		scripts = append(scripts, &temp)
	}

	return c.JSON(http.StatusOK, scripts)
}

func jsShutdown(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return c.JSON(http.StatusOK, map[string]interface{}{
			"testMode": true,
		})
	}

	if myDice.Config.JsEnable {
		myDice.JsShutdown()
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"result": true,
	})
}

func jsStatus(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]interface{}{
		"result": true,
		"status": myDice.Config.JsEnable,
	})
}

func jsEnable(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return c.JSON(200, map[string]interface{}{
			"testMode": true,
		})
	}
	v := struct {
		Name string `form:"name" json:"name"`
	}{}
	err := c.Bind(&v)

	if err == nil {
		dice.JsEnable(myDice, v.Name)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"result": true,
			"name":   v.Name,
		})
	}
	return c.JSON(http.StatusBadRequest, nil)
}

func jsDisable(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return c.JSON(200, map[string]interface{}{
			"testMode": true,
		})
	}
	v := struct {
		Name string `form:"name" json:"name"`
	}{}
	err := c.Bind(&v)

	if err == nil {
		dice.JsDisable(myDice, v.Name)
		return c.JSON(http.StatusOK, map[string]interface{}{
			"result": true,
			"name":   v.Name,
		})
	}

	return c.JSON(http.StatusBadRequest, nil)
}

func jsCheckUpdate(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return Error(&c, "展示模式不支持该操作", Response{"testMode": true})
	}
	v := struct {
		Filename string `json:"filename"`
	}{}
	err := c.Bind(&v)

	if err == nil && v.Filename != "" {
		for _, jsScript := range myDice.JsScriptList {
			if jsScript.Filename == v.Filename {
				oldJs, newJs, tempFileName, errUpdate := myDice.JsCheckUpdate(jsScript)
				if errUpdate != nil {
					return Error(&c, errUpdate.Error(), Response{})
				}
				return Success(&c, Response{
					"old":          oldJs,
					"new":          newJs,
					"format":       "javascript",
					"filename":     jsScript.Filename,
					"tempFileName": tempFileName,
				})
			}
		}
		return Error(&c, "未找到脚本", Response{})
	}
	return Success(&c, Response{})
}

func jsUpdate(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}
	if dm.JustForTest {
		return Error(&c, "展示模式不支持该操作", Response{"testMode": true})
	}
	if !myDice.Config.JsEnable {
		return Error(&c, "js扩展支持已关闭", Response{})
	}

	v := struct {
		Filename     string `json:"filename"`
		TempFileName string `json:"tempFileName"`
	}{}
	err := c.Bind(&v)

	if err == nil && v.Filename != "" {
		for _, jsScript := range myDice.JsScriptList {
			if jsScript.Filename == v.Filename {
				err = myDice.JsUpdate(jsScript, v.TempFileName)
				if err != nil {
					return Error(&c, err.Error(), Response{})
				}
				myDice.MarkModified()
				break
			}
		}
	}
	return Success(&c, Response{})
}
