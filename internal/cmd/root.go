package cmd

import (
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"os"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dave/jennifer/jen"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stoewer/go-strcase"
	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type namingStrategyName string

const (
	none           namingStrategyName = "none"
	camelCase      namingStrategyName = "camelCase"
	pascalCase     namingStrategyName = "PascalCase"
	snakeCase      namingStrategyName = "snake_case"
	upperSnakeCase namingStrategyName = "UPPER_SNAKE_CASE"
	kebabCase      namingStrategyName = "kebab-case"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "go-enumerator",
	Short: "Generate enum-like code for Go constants",
	Long: `Generate enum-like code for Go constants. 

go-enumerator is designed to be called by go generate. See https://pkg.go.dev/github.com/a-jentleman/go-enumerator for usage examples.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cmd.RegisterFlagCompletionFunc("naming-strategy", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			var ret []string

			toComplete = normalizeArg(toComplete)
			if strings.HasPrefix(string(none), toComplete) {
				ret = append(ret, string(none))
			}

			if strings.HasPrefix(string(camelCase), toComplete) {
				ret = append(ret, string(camelCase))
			}

			if strings.HasPrefix(string(pascalCase), toComplete) {
				ret = append(ret, string(pascalCase))
			}

			if strings.HasPrefix(string(snakeCase), toComplete) {
				ret = append(ret, string(snakeCase))
			}

			if strings.HasPrefix(string(upperSnakeCase), toComplete) {
				ret = append(ret, string(upperSnakeCase))
			}

			if strings.HasPrefix(string(kebabCase), toComplete) {
				ret = append(ret, string(kebabCase))
			}

			return ret, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
		})

		inputFileName, ok := resolveParameterValue(cmd.Flag("input"), "GOFILE")
		if !ok {
			return errors.New("failed to determine input file")
		}

		pkgName, ok := resolveParameterValue(cmd.Flag("pkg"), "GOPACKAGE")
		if !ok {
			return errors.New("failed to determine package name")
		}

		pkg, err := loadPackage(pkgName, inputFileName)
		if err != nil {
			return err
		}

		typeName, _ := resolveParameterValue(cmd.Flag("type"), "")

		var line int
		lineStr, _ := resolveParameterValue(cmd.Flag("line"), "GOLINE")
		if lineStr != "" {
			_, err = fmt.Sscan(lineStr, &line)
			if err != nil {
				return fmt.Errorf("failed to determine source line: %w", err)
			}
		}

		tn, err := findTypeDecl(pkg.Fset, pkg.TypesInfo, typeName, inputFileName, line)
		if err != nil {
			return err
		}

		// update typeName if it was not specified by the caller, but we found it in the source code
		if typeName == "" && tn.Name() != "" {
			typeName = tn.Name()
		}

		receiver, _ := resolveParameterValue(cmd.Flag("receiver"), "")
		if receiver == "" {
			receiver = defaultReceiverName(tn)
		}
		receiver = safeIndent(receiver)

		reproCmd := os.Args[0]
		if inputFileName != "" {
			reproCmd = fmt.Sprintf("%s --input=%q", reproCmd, inputFileName)
		}

		if pkgName != "" {
			reproCmd = fmt.Sprintf("%s --pkg=%q", reproCmd, pkgName)
		}

		if line > 0 {
			reproCmd = fmt.Sprintf("%s --line=%d", reproCmd, line)
		}

		vs, kind := findConstantsOfType(pkg.Fset, pkg.TypesInfo, pkg.Syntax, tn, namingStrategyName(flagNameFunc))
		if len(vs) == 0 {
			return fmt.Errorf("no constants of type %q found", tn.Name())
		}

		f, err := generateEnumCode(pkgName, tn, vs, kind, receiver, reproCmd)
		if err != nil {
			return err
		}

		outputFileName, ok := resolveParameterValue(cmd.Flag("output"), "")
		if !ok {
			outputFileName = fmt.Sprintf("%s_enum.go", unexportedName(typeName))
		}

		out, cleanup, err := openOutputFile(outputFileName)
		if err != nil {
			return err
		}
		defer cleanup()

		return f.Render(out)
	},
	Example: "go-enumerator --input example.go --output kind_enum.go --pkg example --type Kind --receiver k",
}

func init() {
	fs := rootCmd.Flags()
	fs.StringVarP(&flagInput, "input", "i", "", "input file to scan. If not specified, input defaults to the value of $GOFILE, which is set by go generate")
	fs.StringVarP(&flagOutput, "output", "o", "", "output file to create. If not specified, output defaults to the value of <type>_enum.go. As special cases, you can specify <STDOUT> or <STDERR> to output to standard output or standard error")
	fs.StringVarP(&flagPkg, "pkg", "p", "", "package name for the generated file. If not specified, pkg defaults to the value of $GOPACKAGE which is set by go generate")
	fs.StringVarP(&flagType, "type", "t", "", "type name to generate an enum definition for. If not specified, it attempts to find the type using $GOLINE and $GOFILE")
	fs.StringVarP(&flagReceiver, "receiver", "r", "", "receiver variable name of the generated methods. By default, the first letter of the type if used")
	fs.IntVarP(&flagLine, "line", "l", 0, "Specify the line to search for types from if a type name is not specified. If not specified, line defaults to the value of $GOLINE which is set by go generate.")
	fs.StringVarP(&flagNameFunc, "naming-strategy", "n", "none", "Specify a naming strategy to use. Valid choices are: none, camelCase, PascalCase, snake_case, UPPER_SNAKE_CASE, and kebab-case. The naming strategy will be used when generating names for enum values. This strategy is ignored for values that have a name override specified as a line comment.")
	_ = fs.MarkHidden("line")
}

var (
	flagInput    string
	flagOutput   string
	flagPkg      string
	flagType     string
	flagReceiver string
	flagLine     int
	flagNameFunc string
)

// resolveParameterValue returns the parameter value from f if it was specified
// by the user. Otherwise, if env is not empty, it looks up the value from the
// environment variable named env.
func resolveParameterValue(f *pflag.Flag, env string) (string, bool) {
	if f.Changed {
		return f.Value.String(), true
	}

	if env != "" {
		return os.LookupEnv(env)
	}

	return f.DefValue, false
}

// loadPackage loads the package of file inputFileName.
func loadPackage(pkgName, inputFileName string) (*packages.Package, error) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedDeps |
			packages.NeedSyntax |
			packages.NeedImports},
		fmt.Sprintf("file=%s", inputFileName))
	if err != nil {
		return nil, err
	}

	var ret *packages.Package
	for _, pkg := range pkgs {
		if pkg.Name != pkgName {
			continue
		}

		if ret != nil {
			return nil, fmt.Errorf("multiple packages found with name %s", pkgName)
		}

		ret = pkg
	}

	if ret == nil {
		return nil, fmt.Errorf("no packages found with name %s", pkgName)
	}

	return ret, nil
}

// findTypeDecl find the relevant *types.TypeName from fset & info.
// If name is passed, a type with that name is searched for.
// Otherwise, the first type after line in inputFileName is returned.
// If the next declaration after line in inputFileName is not a *types.TypeName,
// an error is returned.
func findTypeDecl(fset *token.FileSet, info *types.Info, name, inputFileName string, line int) (*types.TypeName, error) {
	if name != "" {
		return findTypeDeclByName(info, name)
	}

	return findTypeDeclByPosition(fset, info, inputFileName, line)
}

// findTypeDeclByPosition finds the next *type.TypeName in inputFileName after line
func findTypeDeclByPosition(fset *token.FileSet, info *types.Info, inputFileName string, line int) (*types.TypeName, error) {
	var ret *types.TypeName
	var closestObject types.Object
	closest := math.MaxInt32
	for _, object := range info.Defs {
		if object == nil {
			continue
		}

		p := fset.Position(object.Pos())
		if !sameFile(p.Filename, inputFileName) {
			continue
		}

		if p.Line < line || closest < p.Line {
			continue
		}

		ret = nil // we found something closer than our current closest thing
		closestObject = object

		c, ok := object.(*types.TypeName)
		if !ok {
			continue
		}

		ret = c
		closest = p.Line
	}

	if ret == nil {
		if closestObject != nil {
			return nil, fmt.Errorf("failed to determine type: closest declaration is not a named type: %v", closestObject)
		}
		return nil, fmt.Errorf("failed to determine type")
	}

	return ret, nil
}

// findTypeDeclByName finds the the *types.TypeName in info named name.
func findTypeDeclByName(info *types.Info, name string) (*types.TypeName, error) {
	for _, object := range info.Defs {
		if object == nil {
			continue
		}

		c, ok := object.(*types.TypeName)
		if !ok {
			continue
		}

		if c.Name() != name {
			continue
		}

		return c, nil
	}

	return nil, fmt.Errorf("type %q not found", name)
}

type constNameAndString struct {
	Const  *types.Const
	Name   string
	String string
}

// findConstantsOfType finds all constants in info that are of type obj.
func findConstantsOfType(fset *token.FileSet, info *types.Info, syntax []*ast.File, obj types.Object, namingStrategy namingStrategyName) ([]constNameAndString, constant.Kind) {
	var ret []constNameAndString
	kind := constant.Unknown
	for _, object := range info.Defs {
		if object == nil {
			continue
		}

		c, ok := object.(*types.Const)
		if !ok {
			continue
		}

		t, ok := c.Type().(*types.Named)
		if !ok {
			continue
		}

		if c.Name() == "_" {
			continue
		}

		if t.Obj() != obj {
			continue
		}

		k := c.Val().Kind()
		if kind == constant.Unknown {
			kind = k
		}

		if kind != k {
			panic("multiple constant kinds found")
		}

		name := c.Name()
		astFile := findAstFileForToken(c.Pos(), syntax)
		nodes, _ := astutil.PathEnclosingInterval(astFile, c.Pos(), c.Pos())
		str, lineConst, lineOk := findStringInLineComment(c, nodes, astFile, fset)
		if lineOk {
			c = lineConst
		} else {
			switch namingStrategy {
			case camelCase:
				str = strcase.LowerCamelCase(name)
			case pascalCase:
				str = strcase.UpperCamelCase(name)
			case snakeCase:
				str = strcase.SnakeCase(name)
			case upperSnakeCase:
				str = strcase.UpperSnakeCase(name)
			case kebabCase:
				str = strcase.KebabCase(name)
			default:
				str = name
			}
		}

		cn := constNameAndString{
			Const:  c,
			Name:   name,
			String: str,
		}

		ret = append(ret, cn)
	}

	if len(ret) == 0 {
		return nil, constant.Unknown
	}

	// Sort the items based on where they show up in source code.
	// This is mainly to avoid significant differences in version control overtime.
	sort.Slice(ret, func(i, j int) bool {
		ip := fset.Position(ret[i].Const.Pos())
		jp := fset.Position(ret[j].Const.Pos())

		return ip.Filename < jp.Filename ||
			ip.Filename == jp.Filename && ip.Offset < jp.Offset
	})

	return ret, kind
}

func findAstFileForToken(pos token.Pos, syntax []*ast.File) *ast.File {
	for _, file := range syntax {
		if pos < file.FileStart {
			continue
		}
		if pos > file.FileEnd {
			continue
		}
		return file
	}
	return nil
}

func findStringInLineComment(c *types.Const, nodes []ast.Node, astFile *ast.File, tokenFile *token.FileSet) (string, *types.Const, bool) {
	for _, node := range nodes {
		gd, ok := node.(*ast.GenDecl)
		if !ok {
			continue
		}

		for _, cg := range astFile.Comments {
			cgPos := cg.Pos()
			if cgPos < gd.Pos() {
				continue
			}

			cgPosition := tokenFile.Position(cgPos)
			position := tokenFile.Position(c.Pos())
			if cgPosition.Line != position.Line {
				continue
			}

			totalText := cg.Text()
			leftTrimmedText := strings.TrimLeftFunc(totalText, unicode.IsSpace)
			bothTrimmedText := strings.TrimRightFunc(leftTrimmedText, unicode.IsSpace)
			if bothTrimmedText == "" {
				continue
			}

			pos := token.Pos(int(c.Pos()) + len(totalText) - len(leftTrimmedText))
			c := types.NewConst(pos, c.Pkg(), c.Name(), c.Type(), constant.MakeString(bothTrimmedText))

			return bothTrimmedText, c, true
		}
	}
	return "", nil, false
}

// sameFile determines if a and b point to the same file
func sameFile(a, b string) bool {
	as, err := os.Stat(a)
	if err != nil {
		panic(err)
	}

	bs, err := os.Stat(b)
	if err != nil {
		panic(err)
	}

	return os.SameFile(as, bs)
}

// generateEnumCode generates the code to turn tn into an enum
func generateEnumCode(pkgName string, tn *types.TypeName, cs []constNameAndString, kind constant.Kind, receiver string, reproCmd string) (f *jen.File, err error) {
	defer func() {
		if r := recover(); r != nil {
			f = nil
			err = r.(error)
		}
	}()

	tokenVarName := safeIndent("token", receiver)
	stringVarName := safeIndent("str", receiver, tokenVarName)
	scanStateVarName := safeIndent("scanState", receiver, tokenVarName, stringVarName)
	verbVarName := safeIndent("verb", receiver, tokenVarName, stringVarName, scanStateVarName)
	xVarName := safeIndent("x", receiver, tokenVarName, stringVarName, scanStateVarName, verbVarName)

	anyOverrides := false
	uniqueStrings := make(map[string]bool, len(cs))
	uniqueNames := make(map[string]bool, len(cs))
	uniqueValues := make(map[string]bool, len(cs))

	for _, c := range cs {
		if c.String != c.Name {
			anyOverrides = true
		}

		str := c.String
		name := c.Name
		repr := c.Const.Val().ExactString()

		if uniqueStrings[str] {
			return nil, fmt.Errorf("duplicate string found: %q", c.String)
		}

		if uniqueNames[name] {
			return nil, fmt.Errorf("duplicate name found: %q", name)
		}

		if uniqueNames[str] {
			return nil, fmt.Errorf("string collides with existing name: %q", c.String)
		}

		if uniqueValues[repr] {
			return nil, fmt.Errorf("duplicate value found: %s", repr)
		}

		uniqueStrings[str] = true
		uniqueNames[name] = true
		uniqueValues[repr] = true
	}

	f = jen.NewFile(pkgName)
	f.HeaderComment("Code generated by go-enumerator; DO NOT EDIT.")
	f.HeaderComment("Command: " + reproCmd)

	f.Line()
	generateStringMethod(f, receiver, kind, tn, cs, anyOverrides)

	f.Line()
	generateBytesMethod(f, receiver, kind, tn, cs, anyOverrides)

	f.Line()
	generateDefinedMethod(f, receiver, tn, cs)

	f.Line()
	generateScanMethod(f, tn, receiver, scanStateVarName, verbVarName, tokenVarName, cs)

	f.Line()
	generateNextMethod(f, tn, receiver, cs, kind)

	f.Line()
	generateCompileCheckFunction(f, xVarName, cs, kind)

	f.Line()
	generateTextMarshal(f, receiver, tn)

	f.Line()
	generateTextUnmarshal(f, receiver, tn, cs, xVarName)

	f.Line()
	generateTypeAssertions(f, tn, kind)

	f.Line()

	return f, nil
}

// generateCompileCheckFunction generates the _() function that will fail to compile if the constant values have changed.
func generateCompileCheckFunction(f *jen.File, xVarName string, cs []constNameAndString, kind constant.Kind) *jen.Statement {
	return f.Func().Id("_").Params().BlockFunc(func(g *jen.Group) {
		g.Var().Id(xVarName).Index(jen.Lit(1)).Struct()
		g.Comment(`An "invalid array index" compiler error signifies that the constant values have changed.`)
		g.Commentf(`Re-run the %s command to generate them again.`, os.Args[0])
		for _, c := range cs {
			switch kind {
			case constant.String:
				v := constant.StringVal(c.Const.Val())
				g.Line()
				g.Commentf("Begin %q", v)
				for i, b := range []byte(v) {
					g.Id("_").Op("=").Id(xVarName).Index(jen.LitByte(b).Op("-").Id(c.Name).Index(jen.Lit(i)))
				}
			default:
				// using jen.Op here is a bit of a hack, but it allows us to
				// insert the string verbatim without surrounding it with a
				// type cast (as Lit does)
				g.Id("_").Op("=").Id(xVarName).Index(jen.Id(c.Name).Op("-").Op(c.Const.Val().ExactString()))
			}
		}
	})
}

// generateNextMethod generates the Next() method for the enum.
func generateNextMethod(f *jen.File, tn *types.TypeName, receiver string, cs []constNameAndString, kind constant.Kind) {
	var zero interface{} = 0
	if kind == constant.String {
		zero = `""`
	}

	f.Commentf("Next returns the next defined %s. If %s is not defined, then Next returns the first defined value.", tn.Name(), receiver)
	f.Commentf("Next() can be used to loop through all values of an enum.")
	f.Commentf("")
	f.Commentf("\t%s := %s(%v)", receiver, tn.Name(), zero)
	f.Comment("\tfor {")
	f.Commentf("\t\tfmt.Println(%s)", receiver)
	f.Commentf("\t\t%s = %s.Next()", receiver, receiver)
	f.Commentf("\t\tif %s == %s(%v) {", receiver, tn.Name(), zero)
	f.Comment("\t\t\tbreak")
	f.Comment("\t\t}")
	f.Comment("\t}")
	f.Commentf("")
	f.Commentf("The exact order that values are returned when looping should not be relied upon.")
	f.Func().Params(jen.Id(receiver).Id(tn.Name())).Id("Next").Params().Id(tn.Name()).Block(
		jen.Switch(jen.Id(receiver)).BlockFunc(func(g *jen.Group) {
			for i, c := range cs {
				ni := (i + 1) % len(cs)
				g.Case(jen.Id(c.Name)).Block(jen.Return(jen.Id(cs[ni].Name)))
			}
			if len(cs) > 0 {
				g.Default().Block(jen.Return(jen.Id(cs[0].Name)))
			}
		}),
	)
}

// generateScanMethod generates the Scan() method for the enum.
func generateScanMethod(f *jen.File, tn *types.TypeName, receiver string, scanStateVarName string, verbVarName string, tokenVarName string, cs []constNameAndString) {
	f.Commentf("Scan implements [fmt.Scanner]. Use [fmt.Scan] to parse strings into %s values", tn.Name())
	f.Func().Params(jen.Id(receiver).Op("*").Id(tn.Name())).Id("Scan").Params(jen.Id(scanStateVarName).Qual("fmt", "ScanState"), jen.Id(verbVarName).Rune()).Error().Block(
		jen.List(jen.Id(tokenVarName), jen.Err()).Op(":=").Id(scanStateVarName).Dot("Token").Call(jen.True(), jen.Nil()),
		jen.If(jen.Err().Op("!=").Nil()).Block(
			jen.Return(jen.Err()),
		),

		jen.Line(),
		jen.Switch(jen.String().Parens(jen.Id(tokenVarName))).BlockFunc(func(g *jen.Group) {
			for _, c := range cs {
				g.Case(jen.Lit(c.String)).Block(
					jen.Op("*").Id(receiver).Op("=").Id(c.Name),
				)
			}
			g.Default().Block(
				jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit("unknown "+tn.Name()+" value: %s"), jen.Id(tokenVarName))),
			)
		}),

		jen.Return(jen.Nil()),
	)
}

// generateDefinedMethod generates the Defined() method for the enum.
func generateDefinedMethod(f *jen.File, receiver string, tn *types.TypeName, cs []constNameAndString) {
	f.Commentf("Defined returns true if %s holds a defined value.", receiver)
	f.Func().Params(jen.Id(receiver).Id(tn.Name())).Id("Defined").Params().Bool().Block(
		jen.Switch(jen.Id(receiver)).Block(
			jen.CaseFunc(func(g *jen.Group) {
				for _, c := range cs {
					g.Op(c.Const.Val().ExactString())
				}
			}).Block(jen.Return(jen.True())),
			jen.Default().Block(jen.Return(jen.False())),
		),
	)
}

// generateStringMethod generates the String() method for the enum.
func generateStringMethod(f *jen.File, receiver string, kind constant.Kind, eType *types.TypeName, cs []constNameAndString, anyOverrides bool) {
	f.Commentf("String implements [fmt.Stringer]. If !%s.Defined(), then a generated string is returned based on %s's value.", receiver, receiver)
	switch kind {
	case constant.String:

		f.Func().Params(jen.Id(receiver).Id(eType.Name())).Id("String").Params().String().BlockFunc(func(g *jen.Group) {
			if anyOverrides {
				g.Switch(jen.Id(receiver)).BlockFunc(func(g *jen.Group) {
					for _, c := range cs {
						if c.Name == c.String {
							continue
						}

						g.Case(jen.Id(c.Name)).Block(jen.Return(jen.Lit(c.String)))
					}
				})
			}

			g.Return(jen.String().Parens(jen.Id(receiver)))
		})

	default:
		f.Func().Params(jen.Id(receiver).Id(eType.Name())).Id("String").Params().String().Block(
			jen.Switch(jen.Id(receiver)).BlockFunc(func(g *jen.Group) {
				for _, c := range cs {
					g.Case(jen.Id(c.Name)).Block(jen.Return(jen.Lit(c.String)))
				}
			}),
			jen.Return(jen.Qual("fmt", "Sprintf").Call(jen.Lit(fmt.Sprintf("%s(%%d)", eType.Name())), jen.Id(receiver))),
		)
	}
}

// generateBytesMethod generates the Bytes() method for the enum.
func generateBytesMethod(f *jen.File, receiver string, kind constant.Kind, eType *types.TypeName, cs []constNameAndString, anyOverrides bool) {
	f.Commentf("Bytes returns a byte-level representation of String(). If !%s.Defined(), then a generated string is returned based on %s's value.", receiver, receiver)
	switch kind {
	case constant.String:
		f.Func().Params(jen.Id(receiver).Id(eType.Name())).Id("Bytes").Params().Op("[]").Byte().BlockFunc(func(g *jen.Group) {
			if anyOverrides {
				g.Switch(jen.Id(receiver)).BlockFunc(func(g *jen.Group) {
					for _, c := range cs {
						if c.Name == c.String {
							continue
						}

						g.Case(jen.Id(c.Name)).Block(jen.Return(jen.Op("[]").Byte().Parens(jen.Lit(c.String))))
					}
				})
			}
			g.Return(jen.Op("[]").Byte().Parens(jen.Id(receiver)))
		})
	default:
		f.Func().Params(jen.Id(receiver).Id(eType.Name())).Id("Bytes").Params().Op("[]").Byte().Block(
			jen.Switch(jen.Id(receiver)).BlockFunc(func(g *jen.Group) {
				for _, c := range cs {
					g.Case(jen.Id(c.Name)).Block(jen.ReturnFunc(func(g *jen.Group) {
						g.Op("[]").Byte().ValuesFunc(func(g *jen.Group) {
							n := c.String
							for r, size := utf8.DecodeRuneInString(n); len(n) > 0 && r != utf8.RuneError; r, size = utf8.DecodeRuneInString(n) {
								n = n[size:]
								g.LitRune(r)
							}
						})
					}))
				}
			}),
			jen.Return(jen.Op("[]").Byte().Parens(jen.Qual("fmt", "Sprintf").Call(jen.Lit(fmt.Sprintf("%s(%%d)", eType.Name())), jen.Id(receiver)))),
		)
	}
}

func generateTextMarshal(f *jen.File, receiver string, eType *types.TypeName) {
	f.Commentf("MarshalText implements [encoding.TextMarshaler]")
	f.Func().Params(jen.Id(receiver).Id(eType.Name())).Id("MarshalText").Params().Params(jen.Op("[]").Byte(), jen.Error()).Block(
		jen.Return(jen.Id(receiver).Dot("Bytes").Call(), jen.Nil()),
	)
}

func generateTextUnmarshal(f *jen.File, receiver string, eType *types.TypeName, cs []constNameAndString, varName string) {
	f.Commentf("UnmarshalText implements [encoding.TextUnmarshaler]")
	f.Func().Params(jen.Id(receiver).Op("*").Id(eType.Name())).Id("UnmarshalText").Params(jen.Id(varName).Op("[]").Byte()).Params(jen.Error()).Block(
		// This call should be optimized by compiler: https://github.com/golang/go/issues/24937
		jen.Switch(jen.String().Parens(jen.Id(varName))).BlockFunc(func(g *jen.Group) {
			for _, c := range cs {
				g.Case(jen.Lit(c.String)).Block(jen.Op("*").Id(receiver).Op("=").Id(c.Name), jen.Return(jen.Nil()))
			}
			g.Default().Block(jen.Return(jen.Qual("fmt", "Errorf").Call(jen.Lit("failed to parse value %v into %T"), jen.Id(varName), jen.Op("*").Id(receiver))))
		}),
	)
}

func generateTypeAssertions(f *jen.File, eType *types.TypeName, kind constant.Kind) {

	var zero *jen.Statement
	switch kind {
	case constant.String:
		zero = jen.Lit("")
	case constant.Int:
		zero = jen.Lit(0)
	default:
		panic("invalid constant type")
	}

	f.Var().Defs(
		jen.Id("_").Qual("fmt", "Stringer").Op("=").Id(eType.Name()).Parens(zero.Clone()),
		jen.Id("_").Qual("fmt", "Scanner").Op("=").New(jen.Id(eType.Name())),
		jen.Id("_").Qual("encoding", "TextMarshaler").Op("=").Id(eType.Name()).Parens(zero.Clone()),
		jen.Id("_").Qual("encoding", "TextUnmarshaler").Op("=").New(jen.Id(eType.Name())),
	)
}

// defaultReceiverName returns the default receiver name to use for tn
func defaultReceiverName(tn *types.TypeName) string {
	s, _ := utf8.DecodeRuneInString(tn.Name())
	return unexportedName(string(s))
}

// safeIndent returns an identifier that is safe to use (not a keyword,
// and not already used). want is the requested identifier; not is a
// list of identifiers that are already used.
func safeIndent(want string, not ...string) string {
	if token.IsKeyword(want) {
		return safeIndent("_"+want, not...)
	}

	for _, s := range not {
		if want == s {
			return safeIndent("_"+want, not...)
		}
	}

	return want
}

// openOutputFile opens/creates the file to write the output to.
// The returned func is the function to use to "close" the file.
func openOutputFile(name string) (*os.File, func(), error) {
	switch name {
	case "<STDOUT>":
		return os.Stdout, func() { _ = os.Stdout.Sync() }, nil
	case "<STDERR>":
		return os.Stderr, func() { _ = os.Stderr.Sync() }, nil
	default:
		ret, err := os.Create(name)
		if err != nil {
			return nil, nil, err
		}
		return ret, func() { _ = ret.Close() }, nil
	}
}

// unexportedName returns s with the first character replaced
// with its lower case version if it is upper case.
func unexportedName(s string) string {
	if !ast.IsExported(s) {
		return s
	}

	start, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		panic("s is empty")
	}

	start = unicode.ToLower(start)
	return string(start) + s[size:]
}
