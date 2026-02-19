package api

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"

	"sealdice-core/dice"
)

func cleanLocalScriptPath(filename string) string {
	filename = strings.TrimPrefix(filename, "./")
	filename = strings.TrimPrefix(filename, ".\\")
	return filepath.Clean(filename)
}

func isSubPath(path string, root string) bool {
	absPath, err := filepath.Abs(path)
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
	return strings.HasPrefix(absPath, absRoot)
}

func getJsScriptWebUIRoot(jsInfo *dice.JsScriptInfo) string {
	if jsInfo == nil || jsInfo.Filename == "" {
		return ""
	}
	scriptPath := cleanLocalScriptPath(jsInfo.Filename)
	webuiRoot := filepath.Join(filepath.Dir(scriptPath), "webui")
	info, err := os.Stat(webuiRoot)
	if err != nil || !info.IsDir() {
		return ""
	}
	return webuiRoot
}

func getJsScriptWebUIPathByScriptInfo(jsInfo *dice.JsScriptInfo) string {
	if getJsScriptWebUIRoot(jsInfo) == "" {
		return ""
	}
	return "/webui/" + url.PathEscape(jsInfo.Name) + "/"
}

func findJsScriptByPluginName(pluginName string) *dice.JsScriptInfo {
	if myDice == nil || pluginName == "" {
		return nil
	}

	if myDice.JsExtRegistry != nil {
		if ext, ok := myDice.JsExtRegistry.Load(pluginName); ok && ext != nil && ext.Source != nil {
			return ext.Source
		}
	}

	for _, jsInfo := range myDice.JsScriptList {
		if jsInfo.Name == pluginName {
			return jsInfo
		}
	}
	return nil
}

func getJsScriptWebUIPathByPluginName(pluginName string) string {
	jsInfo := findJsScriptByPluginName(pluginName)
	if jsInfo == nil {
		return ""
	}
	if getJsScriptWebUIRoot(jsInfo) == "" {
		return ""
	}
	return "/webui/" + url.PathEscape(pluginName) + "/"
}

func jsPluginWebUI(c echo.Context) error {
	pluginName, err := url.PathUnescape(c.Param("plugin"))
	if err != nil || strings.TrimSpace(pluginName) == "" {
		return c.NoContent(http.StatusNotFound)
	}

	jsInfo := findJsScriptByPluginName(pluginName)
	if jsInfo == nil {
		return c.NoContent(http.StatusNotFound)
	}
	webuiRoot := getJsScriptWebUIRoot(jsInfo)
	if webuiRoot == "" {
		return c.NoContent(http.StatusNotFound)
	}

	reqPath := strings.TrimPrefix(c.Param("*"), "/")
	if reqPath == "" {
		indexPath := filepath.Join(webuiRoot, "index.html")
		if _, err = os.Stat(indexPath); err == nil {
			return c.File(indexPath)
		}
		return c.NoContent(http.StatusNotFound)
	}

	target := filepath.Join(webuiRoot, filepath.FromSlash(reqPath))
	if !isSubPath(target, webuiRoot) {
		return c.NoContent(http.StatusForbidden)
	}

	info, statErr := os.Stat(target)
	if statErr == nil {
		if info.IsDir() {
			indexPath := filepath.Join(target, "index.html")
			if _, err = os.Stat(indexPath); err == nil {
				return c.File(indexPath)
			}
		} else {
			return c.File(target)
		}
	}

	// SPA fallback.
	indexPath := filepath.Join(webuiRoot, "index.html")
	if _, err = os.Stat(indexPath); err == nil {
		return c.File(indexPath)
	}
	return c.NoContent(http.StatusNotFound)
}

func jsWebUIConfigGet(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}

	pluginName := strings.TrimSpace(c.QueryParam("pluginName"))
	if pluginName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pluginName is required")
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"pluginName": pluginName,
		"values":     myDice.ConfigManager.GetPluginWebUIConfigs(pluginName),
	})
}

type jsWebUIConfigSetReq struct {
	PluginName string                 `json:"pluginName"`
	Values     map[string]interface{} `json:"values"`
}

func jsWebUIConfigSet(c echo.Context) error {
	if !doAuth(c) {
		return c.JSON(http.StatusForbidden, nil)
	}

	var v jsWebUIConfigSetReq
	if err := c.Bind(&v); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Failed to parse data")
	}
	v.PluginName = strings.TrimSpace(v.PluginName)
	if v.PluginName == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "pluginName is required")
	}

	if err := myDice.ConfigManager.SetPluginWebUIConfigs(v.PluginName, v.Values); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.JSON(http.StatusOK, map[string]interface{}{
		"pluginName": v.PluginName,
		"values":     myDice.ConfigManager.GetPluginWebUIConfigs(v.PluginName),
	})
}
