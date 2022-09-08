package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/AkaiNoCat/Practice-Go/week_03/gen/annotation"
	"github.com/AkaiNoCat/Practice-Go/week_03/gen/http"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// 实际上 main 函数这里要考虑接收参数
// src 源目标
// dst 目标目录
// type src 里面可能有很多类型，那么用户可能需要指定具体的类型
// 这里我们简化操作，只读取当前目录下的数据，并且扫描下面的所有源文件，然后生成代码
// 在当前目录下运行 go install 就将 main 安装成功了，
// 可以在命令行中运行 gen
// 在 testdata 里面运行 gen，则会生成能够通过所有测试的代码
func main() {
	err := gen(".")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("success")
}

func gen(src string) error {
	// 第一步找出符合条件的文件
	srcFiles, err := scanFiles(src)
	if err != nil {
		return err
	}
	// 第二步，AST 解析源代码文件，拿到 service definition 定义
	defs, err := parseFiles(srcFiles)
	if err != nil {
		return err
	}
	// 生成代码
	return genFiles(src, defs)
}

// 根据 defs 来生成代码
// src 是源代码所在目录，在测试里面它是 ./testdata
func genFiles(src string, defs []http.ServiceDefinition) error {
	for _, d := range defs {
		filename := underscoreName(d.GenName())
		filename = filename + ".go"
		filePath, _ := filepath.Abs(src + "/" + filename)
		bs := &bytes.Buffer{}
		http.Gen(bs, d)
		b := bs.Bytes()
		bs2, _ := format.Source(b)
		txt := string(bs2)
		// print to file
		f, err := os.Create(filePath)
		if err != nil {
			return err
		}
		defer f.Close()
		w := bufio.NewWriter(f)
		_, err = w.WriteString(txt)
		if err != nil {
			return err
		}
		w.Flush()
	}
	//	var p1file *os.File
	//	p1file, _ = os.OpenFile(filePath, os.O_APPEND|os.O_CREATE, 0666)
	//	defer p1file.Close()                //延迟关闭，写在前面防止忘了
	//	p1writer := bufio.NewWriter(p1file) //使用带缓存的*Writer
	//	p1writer.WriteString(txt2)          //在调用WriterString方法时，内容先写入缓存
	//	p1writer.Flush()                    //调用flush方法，将缓存的数据真正写入到文件中
	//}
	return nil
}

func parseFiles(srcFiles []string) ([]http.ServiceDefinition, error) {
	defs := make([]http.ServiceDefinition, 0, 20)
	for _, src := range srcFiles {
		fmt.Println(src)
		// 你需要利用 annotation 里面的东西来扫描 src，然后生成 file
		var file annotation.File
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, src, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		tv := &annotation.SingleFileEntryVisitor{}
		ast.Walk(tv, f)
		file = tv.Get()
		for _, typ := range file.Types {
			_, ok := typ.Annotations.Get("HttpClient")
			if !ok {
				continue
			}
			def, err := parseServiceDefinition(file.Node.Name.Name, typ)
			if err != nil {
				return nil, err
			}
			defs = append(defs, def)
		}
	}
	return defs, nil
}

// 你需要利用 typ 来构造一个 http.ServiceDefinition
// 注意你可能需要检测用户的定义是否符合你的预期
func parseServiceDefinition(pkg string, typ annotation.Type) (http.ServiceDefinition, error) {
	var methods []http.ServiceMethod
	def := http.ServiceDefinition{}
	def.Package = pkg
	name, ok := typ.Annotations.Get("ServiceName")
	if ok {
		def.Name = name.Value
	} else {
		def.Name = typ.Annotations.Node.Name.Name
	}
	//for _, method := range typ.Node.Name.Obj.Decl.(*ast.TypeSpec).Type.(*ast.InterfaceType).Methods.List {
	for _, Field := range typ.Fields {
		var med http.ServiceMethod
		med.Name = Field.Annotations.Node.Names[0].Name
		var path string
		ansPath, ok := Field.Annotations.Get("Path")
		if ok {
			path = ansPath.Value
		} else {
			path = "/" + med.Name
		}
		med.Path = path
		if len(Field.Annotations.Node.Type.(*ast.FuncType).Params.List) != 2 {
			return def, fmt.Errorf("gen: 方法必须接收两个参数，其中第一个参数是 context.Context，第二个参数请求")
		}
		if len(Field.Annotations.Node.Type.(*ast.FuncType).Results.List) != 2 {
			return def, fmt.Errorf("gen: 方法必须返回两个参数，其中第一个返回值是响应，第二个返回值是error")
		}
		for _, Param := range Field.Annotations.Node.Type.(*ast.FuncType).Params.List {
			par, ok := Param.Type.(*ast.StarExpr)
			if ok {
				p, ok := par.X.(*ast.Ident)
				if ok {
					med.ReqTypeName = p.Name
					continue
				}
			}
		}
		for _, Result := range Field.Annotations.Node.Type.(*ast.FuncType).Results.List {
			res, ok := Result.Type.(*ast.StarExpr)
			if ok {
				r, ok := res.X.(*ast.Ident)
				if ok {
					med.RespTypeName = r.Name
					continue
				}
			}
		}
		methods = append(methods, med)
	}
	def.Methods = methods
	return def, nil
}

// 返回符合条件的 Go 源代码文件，也就是你要用 AST 来分析这些文件的代码
func scanFiles(src string) ([]string, error) {
	// 找出符合条件的文件
	// 你需要利用 os 包来扫描 src 目录下的所有文件
	files, err := os.ReadDir(src)
	if err != nil {
		return nil, err
	}
	srcFiles := make([]string, 0, 20)
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		if file.Type().IsRegular() && file.Name() != "main.go" && !strings.HasSuffix(file.Name(), "gen.go") && strings.HasSuffix(file.Name(), ".go") {
			fileabs, _ := filepath.Abs(src + "/" + file.Name())
			srcFiles = append(srcFiles, fileabs)
		}
	}
	return srcFiles, nil
}

// underscoreName 驼峰转字符串命名，在决定生成的文件名的时候需要这个方法
// 可以用正则表达式，然而我写不出来，我是正则渣
func underscoreName(name string) string {
	var buf []byte
	for i, v := range name {
		if unicode.IsUpper(v) {
			if i != 0 {
				buf = append(buf, '_')
			}
			buf = append(buf, byte(unicode.ToLower(v)))
		} else {
			buf = append(buf, byte(v))
		}

	}
	return string(buf)
}
