package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// CalculatorTool evaluates arithmetic, percentages, math functions, and algebraic equations.
type CalculatorTool struct{}

// NewCalculatorTool creates a new calculator tool instance.
func NewCalculatorTool() *CalculatorTool {
	return &CalculatorTool{}
}

func (c *CalculatorTool) Name() string {
	return "calculator"
}

func (c *CalculatorTool) Description() string {
	return "حاسبة متطورة لحساب العمليات الحسابية، النسب المئوية، الدوال الرياضية المتقدمة، وحل المعادلات الخطية والتربيعية بدقة متناهية"
}

func (c *CalculatorTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"expression": map[string]interface{}{
				"type":        "string",
				"description": "العملية الحسابية أو المعادلة أو النسبة المئوية (مثال: '500 + 15%', '2x + 5 = 15', 'x^2 - 5x + 6 = 0', 'sqrt(144) + 10^2', '20% of 500')",
			},
		},
		"required": []string{"expression"},
	}
}

func (c *CalculatorTool) Permission() PermissionLevel {
	return PermissionEveryone
}

type calculatorArgs struct {
	Expression string `json:"expression"`
}

func (c *CalculatorTool) Execute(ctx context.Context, args json.RawMessage, execCtx ExecutionContext) (string, error) {
	var input calculatorArgs
	if err := json.Unmarshal(args, &input); err != nil {
		// Fallback: try raw string
		input.Expression = strings.Trim(string(args), `"{}`)
	}

	expr := strings.TrimSpace(input.Expression)
	if expr == "" {
		return "", errors.New("expression cannot be empty")
	}

	result, err := EvaluateMathOrEquation(expr)
	if err != nil {
		return fmt.Sprintf("❌ خطأ في الحساب: %v", err), nil
	}

	return result, nil
}

// NormalizeArabicMath converts Arabic numerals and symbols into standard math notations.
func NormalizeArabicMath(input string) string {
	var sb strings.Builder
	for _, r := range input {
		switch r {
		case '٠':
			sb.WriteRune('0')
		case '١':
			sb.WriteRune('1')
		case '٢':
			sb.WriteRune('2')
		case '٣':
			sb.WriteRune('3')
		case '٤':
			sb.WriteRune('4')
		case '٥':
			sb.WriteRune('5')
		case '٦':
			sb.WriteRune('6')
		case '٧':
			sb.WriteRune('7')
		case '٨':
			sb.WriteRune('8')
		case '٩':
			sb.WriteRune('9')
		case '×':
			sb.WriteRune('*')
		case '÷':
			sb.WriteRune('/')
		case '−', '—', '–':
			sb.WriteRune('-')
		case '٪':
			sb.WriteRune('%')
		case '،':
			sb.WriteRune(',')
		default:
			sb.WriteRune(r)
		}
	}

	str := sb.String()
	str = strings.ReplaceAll(str, "جذر", "sqrt")
	str = strings.ReplaceAll(str, "مطلق", "abs")
	str = strings.ReplaceAll(str, "لوغاريتم", "log10")
	str = strings.ReplaceAll(str, "في المية", "%")
	str = strings.ReplaceAll(str, "في المئة", "%")
	str = strings.ReplaceAll(str, "بالمية", "%")
	str = strings.ReplaceAll(str, "بالمئة", "%")
	str = strings.ReplaceAll(str, "من", "of")
	return str
}

// EvaluateMathOrEquation handles equations, percentages, and arbitrary math expressions.
func EvaluateMathOrEquation(rawExpr string) (string, error) {
	norm := NormalizeArabicMath(rawExpr)
	norm = strings.TrimSpace(norm)

	// 1. Check if it's an equation containing '='
	if strings.Contains(norm, "=") {
		parts := strings.Split(norm, "=")
		if len(parts) == 2 {
			lhs := strings.TrimSpace(parts[0])
			rhs := strings.TrimSpace(parts[1])
			if (containsVariable(lhs) || containsVariable(rhs)) && lhs != "" && rhs != "" {
				return SolveAlgebraicEquation(lhs, rhs)
			}
		}
	}

	// 2. Check for Percentage patterns
	// Pattern: "X as % of Y" or "what % of Y is X" or "X / Y as %" or "X of Y as %"
	if res, matched, err := evaluatePercentageOf(norm); matched {
		return res, err
	}

	// Pattern: "P% of X" or "P% * X"
	if res, matched, err := evaluatePercentageDiscountOrPortion(norm); matched {
		return res, err
	}

	// Pattern: "X + P%" or "X - P%"
	if res, matched, err := evaluatePercentageAdditionSubtraction(norm); matched {
		return res, err
	}

	// 3. General mathematical expression evaluation
	val, err := EvaluateExpression(norm)
	if err != nil {
		return "", err
	}

	return formatCalculationResult(rawExpr, val), nil
}

func containsVariable(s string) bool {
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' {
			// Exclude known function names
			// We check simple single letters 'x', 'y', 'z', 'n'
			if r == 'x' || r == 'y' || r == 'z' || r == 'n' {
				return true
			}
		}
	}
	return false
}

func formatCalculationResult(origExpr string, val float64) string {
	formattedVal := formatFloat(val)

	var sb strings.Builder
	sb.WriteString("🧮 **العملية الحسابية:** `")
	sb.WriteString(origExpr)
	sb.WriteString("`\n")
	sb.WriteString("📊 **النتيجة:** `")
	sb.WriteString(formattedVal)
	sb.WriteString("`")

	// If integer with large magnitude or float with lots of decimals, format readable
	if math.Abs(val) >= 1e6 || (math.Abs(val) < 1e-4 && val != 0) {
		sb.WriteString(fmt.Sprintf("\n🔬 **بالصيغة العلمية:** `%.6e`", val))
	}

	return sb.String()
}

func formatFloat(f float64) string {
	if math.IsNaN(f) {
		return "NaN (غير معرف)"
	}
	if math.IsInf(f, 1) {
		return "+∞ (مالانهاية موجبة)"
	}
	if math.IsInf(f, -1) {
		return "-∞ (مالانهاية سالبة)"
	}
	if math.Floor(f) == f && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	res := strconv.FormatFloat(f, 'f', 6, 64)
	res = strings.TrimRight(res, "0")
	res = strings.TrimRight(res, ".")
	return res
}

// Percentage Handlers
func evaluatePercentageOf(expr string) (string, bool, error) {
	// Pattern 1: "what % of 200 is 50" -> total=200, part=50
	re1 := regexp.MustCompile(`(?i)^\s*(?:what\s+%\s+of\s+|percentage\s+of\s+)([\d\.]+)\s+is\s+([\d\.]+)\s*$`)
	if m := re1.FindStringSubmatch(expr); len(m) == 3 {
		total, err1 := strconv.ParseFloat(m[1], 64)
		part, err2 := strconv.ParseFloat(m[2], 64)
		if err1 == nil && err2 == nil {
			if total == 0 {
				return "", true, errors.New("cannot divide by zero in percentage calculation")
			}
			pct := (part / total) * 100
			return fmt.Sprintf("📊 **حساب النسبة المئوية:**\nالعدد `%.4g` يمثل **`%s%%`** من الإجمالي `%.4g`", part, formatFloat(pct), total), true, nil
		}
	}

	// Pattern 2: "50 as % of 200", "50 as percentage of 200" -> part=50, total=200
	re2 := regexp.MustCompile(`(?i)^\s*([\d\.]+)\s+as\s+(?:%|percentage)\s+of\s+([\d\.]+)\s*$`)
	if m := re2.FindStringSubmatch(expr); len(m) == 3 {
		part, err1 := strconv.ParseFloat(m[1], 64)
		total, err2 := strconv.ParseFloat(m[2], 64)
		if err1 == nil && err2 == nil {
			if total == 0 {
				return "", true, errors.New("cannot divide by zero in percentage calculation")
			}
			pct := (part / total) * 100
			return fmt.Sprintf("📊 **حساب النسبة المئوية:**\nالعدد `%.4g` يمثل **`%s%%`** من الإجمالي `%.4g`", part, formatFloat(pct), total), true, nil
		}
	}

	// Pattern 3: "50 / 200 as %", "50 of 200 as %", "50 from 200 as %" -> part=50, total=200
	re3 := regexp.MustCompile(`(?i)^\s*([\d\.]+)\s*(?:\/|of|from)\s*([\d\.]+)\s+(?:as\s+%|in\s+%|as\s+percentage)\s*$`)
	if m := re3.FindStringSubmatch(expr); len(m) == 3 {
		part, err1 := strconv.ParseFloat(m[1], 64)
		total, err2 := strconv.ParseFloat(m[2], 64)
		if err1 == nil && err2 == nil {
			if total == 0 {
				return "", true, errors.New("cannot divide by zero in percentage calculation")
			}
			pct := (part / total) * 100
			return fmt.Sprintf("📊 **حساب النسبة المئوية:**\nالعدد `%.4g` يمثل **`%s%%`** من الإجمالي `%.4g`", part, formatFloat(pct), total), true, nil
		}
	}

	return "", false, nil
}

func evaluatePercentageDiscountOrPortion(expr string) (string, bool, error) {
	// matches "20% of 500", "20% * 500", "20 % of 500"
	re := regexp.MustCompile(`(?i)^\s*([\d\.]+)\s*%\s*(?:of|\*)\s*([\d\.]+)\s*$`)
	if m := re.FindStringSubmatch(expr); len(m) == 3 {
		pct, err1 := strconv.ParseFloat(m[1], 64)
		total, err2 := strconv.ParseFloat(m[2], 64)
		if err1 == nil && err2 == nil {
			result := (pct / 100.0) * total
			return fmt.Sprintf("📊 **حساب النسبة:**\n`%s%%` من `%.4g` = **`%s`**", formatFloat(pct), total, formatFloat(result)), true, nil
		}
	}
	return "", false, nil
}

func evaluatePercentageAdditionSubtraction(expr string) (string, bool, error) {
	// matches "500 + 15%", "500 - 20%"
	re := regexp.MustCompile(`(?i)^\s*([\d\.]+)\s*([\+\-])\s*([\d\.]+)\s*%\s*$`)
	if m := re.FindStringSubmatch(expr); len(m) == 4 {
		base, err1 := strconv.ParseFloat(m[1], 64)
		op := m[2]
		pct, err2 := strconv.ParseFloat(m[3], 64)
		if err1 == nil && err2 == nil {
			portion := base * (pct / 100.0)
			var total float64
			opName := "زيادة"
			if op == "+" {
				total = base + portion
			} else {
				total = base - portion
				opName = "خصم"
			}
			return fmt.Sprintf("📊 **حساب %s النسبة المئوية:**\nالمبلغ الأصلي: `%s`\nقيمة النسبة (%s%%): `%s`\n**الناتج النهائي:** **`%s`**",
				opName, formatFloat(base), formatFloat(pct), formatFloat(portion), formatFloat(total)), true, nil
		}
	}
	return "", false, nil
}

// -------------------------------------------------------------
// Algebraic Equation Solver (Linear & Quadratic)
// -------------------------------------------------------------

// Polynomial represents a*x^2 + b*x + c
type Polynomial struct {
	A float64 // x^2 coefficient
	B float64 // x coefficient
	C float64 // constant term
}

func (p *Polynomial) Add(other Polynomial) {
	p.A += other.A
	p.B += other.B
	p.C += other.C
}

func (p *Polynomial) Sub(other Polynomial) {
	p.A -= other.A
	p.B -= other.B
	p.C -= other.C
}

func parsePolynomial(side string) (Polynomial, error) {
	side = strings.ReplaceAll(side, " ", "")
	if side == "" {
		return Polynomial{}, errors.New("empty equation side")
	}

	// Normalize variable name to 'x'
	side = strings.ReplaceAll(side, "y", "x")
	side = strings.ReplaceAll(side, "z", "x")
	side = strings.ReplaceAll(side, "n", "x")
	side = strings.ReplaceAll(side, "X", "x")

	// Insert '+' before '-' for splitting terms, except at start or after operator
	var sanitized strings.Builder
	for i, r := range side {
		if r == '-' && i > 0 && side[i-1] != '+' && side[i-1] != '-' && side[i-1] != '*' && side[i-1] != '/' && side[i-1] != '^' && side[i-1] != '(' {
			sanitized.WriteRune('+')
		}
		sanitized.WriteRune(r)
	}

	terms := strings.Split(sanitized.String(), "+")
	var poly Polynomial

	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}

		// Quadratic term (contains x^2 or x2)
		if strings.Contains(term, "x^2") || strings.Contains(term, "x²") || strings.Contains(term, "x2") {
			coeffStr := term
			coeffStr = strings.ReplaceAll(coeffStr, "x^2", "")
			coeffStr = strings.ReplaceAll(coeffStr, "x²", "")
			coeffStr = strings.ReplaceAll(coeffStr, "x2", "")
			coeffStr = strings.ReplaceAll(coeffStr, "*", "")

			var coeff float64 = 1.0
			if coeffStr == "" || coeffStr == "+" {
				coeff = 1.0
			} else if coeffStr == "-" {
				coeff = -1.0
			} else {
				c, err := strconv.ParseFloat(coeffStr, 64)
				if err != nil {
					return poly, fmt.Errorf("invalid quadratic term coefficient %q", term)
				}
				coeff = c
			}
			poly.A += coeff
		} else if strings.Contains(term, "x") {
			// Linear term
			coeffStr := strings.ReplaceAll(term, "x", "")
			coeffStr = strings.ReplaceAll(coeffStr, "*", "")

			var coeff float64 = 1.0
			if coeffStr == "" || coeffStr == "+" {
				coeff = 1.0
			} else if coeffStr == "-" {
				coeff = -1.0
			} else {
				c, err := strconv.ParseFloat(coeffStr, 64)
				if err != nil {
					return poly, fmt.Errorf("invalid linear term coefficient %q", term)
				}
				coeff = c
			}
			poly.B += coeff
		} else {
			// Constant term
			c, err := strconv.ParseFloat(term, 64)
			if err != nil {
				// Try evaluating constant expression (e.g. 5*2)
				val, evalErr := EvaluateExpression(term)
				if evalErr != nil {
					return poly, fmt.Errorf("invalid constant term %q: %v", term, err)
				}
				c = val
			}
			poly.C += c
		}
	}

	return poly, nil
}

// SolveAlgebraicEquation solves linear and quadratic equations.
func SolveAlgebraicEquation(lhsStr, rhsStr string) (string, error) {
	lhs, err := parsePolynomial(lhsStr)
	if err != nil {
		return "", fmt.Errorf("خطأ في تحليل الطرف الأيسر: %w", err)
	}

	rhs, err := parsePolynomial(rhsStr)
	if err != nil {
		return "", fmt.Errorf("خطأ في تحليل الطرف الأيمن: %w", err)
	}

	// Rearrange to LHS - RHS = 0: a*x^2 + b*x + c = 0
	a := lhs.A - rhs.A
	b := lhs.B - rhs.B
	c := lhs.C - rhs.C

	origEq := fmt.Sprintf("%s = %s", lhsStr, rhsStr)

	// Case 1: Quadratic equation (a != 0)
	if math.Abs(a) > 1e-9 {
		discriminant := (b * b) - (4 * a * c)
		var sb strings.Builder
		sb.WriteString("📐 **حل المعادلة التربيعية:** `")
		sb.WriteString(origEq)
		sb.WriteString("`\n")
		sb.WriteString(fmt.Sprintf("الصورة القياسية: `%s x² %s %s x %s %s = 0`\n",
			formatFloat(a), signStr(b), formatFloat(math.Abs(b)), signStr(c), formatFloat(math.Abs(c))))
		sb.WriteString(fmt.Sprintf("المميز (Δ = b² - 4ac): `%s`\n", formatFloat(discriminant)))

		if discriminant > 1e-9 {
			sqrtD := math.Sqrt(discriminant)
			x1 := (-b + sqrtD) / (2 * a)
			x2 := (-b - sqrtD) / (2 * a)
			sb.WriteString(fmt.Sprintf("✅ **يوجد حلان حقيقيان:**\n• **x₁ = %s**\n• **x₂ = %s**", formatFloat(x1), formatFloat(x2)))
		} else if math.Abs(discriminant) <= 1e-9 {
			x := -b / (2 * a)
			sb.WriteString(fmt.Sprintf("✅ **يوجد حل حقيقي وحيد (مكرر):**\n• **x = %s**", formatFloat(x)))
		} else {
			// Complex roots
			realPart := -b / (2 * a)
			imagPart := math.Sqrt(-discriminant) / (2 * a)
			sb.WriteString(fmt.Sprintf("🔮 **يوجد حلان مركبان (تخيليان):**\n• **x₁ = %s + %si**\n• **x₂ = %s - %si**",
				formatFloat(realPart), formatFloat(math.Abs(imagPart)),
				formatFloat(realPart), formatFloat(math.Abs(imagPart))))
		}
		return sb.String(), nil
	}

	// Case 2: Linear equation (a == 0, b != 0)
	if math.Abs(b) > 1e-9 {
		x := -c / b
		return fmt.Sprintf("📐 **حل المعادلة الخطية:** `%s`\n✅ **قيمة المجهول:** **x = %s**", origEq, formatFloat(x)), nil
	}

	// Case 3: Degenerate equation (a == 0, b == 0)
	if math.Abs(c) < 1e-9 {
		return fmt.Sprintf("📐 **المعادلة:** `%s`\n✅ **متطابقة صحيحة دائماً لجميع قيم x** (جميع الأعداد الحقيقية ℝ).", origEq), nil
	}

	return fmt.Sprintf("📐 **المعادلة:** `%s`\n❌ **معادلة مستحيلة الحل** (ليس لها حل لأن %s ≠ 0).", origEq, formatFloat(c)), nil
}

func signStr(val float64) string {
	if val >= 0 {
		return "+"
	}
	return "-"
}

// -------------------------------------------------------------
// Mathematical Expression AST / Shunting-Yard Parser
// -------------------------------------------------------------

type tokenType int

const (
	tokNumber tokenType = iota
	tokOperator
	tokFunction
	tokLParen
	tokRParen
	tokComma
)

type token struct {
	typ tokenType
	val string
	num float64
}

func tokenize(expr string) ([]token, error) {
	var tokens []token
	runes := []rune(expr)
	n := len(runes)
	i := 0

	for i < n {
		r := runes[i]

		if unicode.IsSpace(r) {
			i++
			continue
		}

		if r == '(' {
			tokens = append(tokens, token{typ: tokLParen, val: "("})
			i++
			continue
		}
		if r == ')' {
			tokens = append(tokens, token{typ: tokRParen, val: ")"})
			i++
			continue
		}
		if r == ',' {
			tokens = append(tokens, token{typ: tokComma, val: ","})
			i++
			continue
		}

		if r == '+' || r == '-' || r == '*' || r == '/' || r == '%' || r == '^' || r == '!' {
			// Check for unary minus / plus
			isUnary := false
			if (r == '-' || r == '+') && (len(tokens) == 0 ||
				tokens[len(tokens)-1].typ == tokLParen ||
				tokens[len(tokens)-1].typ == tokOperator ||
				tokens[len(tokens)-1].typ == tokComma) {
				isUnary = true
			}

			if isUnary {
				if r == '-' {
					tokens = append(tokens, token{typ: tokOperator, val: "u-"})
				} else {
					tokens = append(tokens, token{typ: tokOperator, val: "u+"})
				}
			} else {
				tokens = append(tokens, token{typ: tokOperator, val: string(r)})
			}
			i++
			continue
		}

		// Number parsing
		if unicode.IsDigit(r) || (r == '.' && i+1 < n && unicode.IsDigit(runes[i+1])) {
			start := i
			hasDot := false
			for i < n && (unicode.IsDigit(runes[i]) || runes[i] == '.' || runes[i] == 'e' || runes[i] == 'E') {
				if runes[i] == '.' {
					if hasDot {
						break
					}
					hasDot = true
				}
				if runes[i] == 'e' || runes[i] == 'E' {
					if i+1 < n && (runes[i+1] == '+' || runes[i+1] == '-') {
						i++
					}
				}
				i++
			}
			numStr := string(runes[start:i])
			numVal, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid number %q", numStr)
			}
			tokens = append(tokens, token{typ: tokNumber, val: numStr, num: numVal})
			continue
		}

		// Identifiers (functions or constants)
		if unicode.IsLetter(r) || r == '_' {
			start := i
			for i < n && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			ident := strings.ToLower(string(runes[start:i]))

			switch ident {
			case "pi", "π":
				tokens = append(tokens, token{typ: tokNumber, val: "pi", num: math.Pi})
			case "e":
				tokens = append(tokens, token{typ: tokNumber, val: "e", num: math.E})
			case "tau", "τ":
				tokens = append(tokens, token{typ: tokNumber, val: "tau", num: 2 * math.Pi})
			case "phi", "φ":
				tokens = append(tokens, token{typ: tokNumber, val: "phi", num: 1.6180339887498948482})
			default:
				tokens = append(tokens, token{typ: tokFunction, val: ident})
			}
			continue
		}

		return nil, fmt.Errorf("unrecognized character %q", string(r))
	}

	return tokens, nil
}

func opPrecedence(op string) (int, bool) { // precedence, isRightAssociative
	switch op {
	case "u-", "u+":
		return 5, true
	case "^":
		return 4, true
	case "!", "%":
		return 3, false
	case "*", "/":
		return 2, false
	case "+", "-":
		return 1, false
	}
	return 0, false
}

// EvaluateExpression computes mathematical expressions using the Shunting-Yard algorithm.
func EvaluateExpression(expr string) (float64, error) {
	tokens, err := tokenize(expr)
	if err != nil {
		return 0, err
	}
	if len(tokens) == 0 {
		return 0, errors.New("empty expression")
	}

	var output []token
	var opStack []token

	for _, tok := range tokens {
		switch tok.typ {
		case tokNumber:
			output = append(output, tok)

		case tokFunction:
			opStack = append(opStack, tok)

		case tokComma:
			for len(opStack) > 0 && opStack[len(opStack)-1].typ != tokLParen {
				output = append(output, opStack[len(opStack)-1])
				opStack = opStack[:len(opStack)-1]
			}
			if len(opStack) == 0 {
				return 0, errors.New("misplaced comma or missing parenthesis")
			}

		case tokOperator:
			prec1, rightAssoc1 := opPrecedence(tok.val)
			for len(opStack) > 0 && opStack[len(opStack)-1].typ == tokOperator {
				topOp := opStack[len(opStack)-1].val
				prec2, _ := opPrecedence(topOp)
				if (rightAssoc1 && prec1 < prec2) || (!rightAssoc1 && prec1 <= prec2) {
					output = append(output, opStack[len(opStack)-1])
					opStack = opStack[:len(opStack)-1]
				} else {
					break
				}
			}
			opStack = append(opStack, tok)

		case tokLParen:
			opStack = append(opStack, tok)

		case tokRParen:
			for len(opStack) > 0 && opStack[len(opStack)-1].typ != tokLParen {
				output = append(output, opStack[len(opStack)-1])
				opStack = opStack[:len(opStack)-1]
			}
			if len(opStack) == 0 {
				return 0, errors.New("unmatched closing parenthesis ')'")
			}
			opStack = opStack[:len(opStack)-1] // pop '('

			// If top of stack is function, pop to output
			if len(opStack) > 0 && opStack[len(opStack)-1].typ == tokFunction {
				output = append(output, opStack[len(opStack)-1])
				opStack = opStack[:len(opStack)-1]
			}
		}
	}

	for len(opStack) > 0 {
		top := opStack[len(opStack)-1]
		if top.typ == tokLParen || top.typ == tokRParen {
			return 0, errors.New("unmatched opening parenthesis '('")
		}
		output = append(output, top)
		opStack = opStack[:len(opStack)-1]
	}

	// Evaluate RPN
	var valStack []float64
	for _, tok := range output {
		switch tok.typ {
		case tokNumber:
			valStack = append(valStack, tok.num)

		case tokOperator:
			if tok.val == "u-" {
				if len(valStack) < 1 {
					return 0, errors.New("missing operand for unary minus")
				}
				valStack[len(valStack)-1] = -valStack[len(valStack)-1]
				continue
			}
			if tok.val == "u+" {
				continue
			}
			if tok.val == "!" {
				if len(valStack) < 1 {
					return 0, errors.New("missing operand for factorial")
				}
				n := valStack[len(valStack)-1]
				if n < 0 || math.Floor(n) != n {
					return 0, errors.New("factorial is only defined for non-negative integers")
				}
				valStack[len(valStack)-1] = factorial(int64(n))
				continue
			}

			if len(valStack) < 2 {
				return 0, fmt.Errorf("missing operands for operator %q", tok.val)
			}
			b := valStack[len(valStack)-1]
			a := valStack[len(valStack)-2]
			valStack = valStack[:len(valStack)-2]

			var res float64
			switch tok.val {
			case "+":
				res = a + b
			case "-":
				res = a - b
			case "*":
				res = a * b
			case "/":
				if b == 0 {
					return 0, errors.New("division by zero (القسمة على صفر غير جائزة)")
				}
				res = a / b
			case "%":
				if b == 0 {
					return 0, errors.New("modulo by zero")
				}
				res = math.Mod(a, b)
			case "^":
				res = math.Pow(a, b)
			default:
				return 0, fmt.Errorf("unknown operator %q", tok.val)
			}
			valStack = append(valStack, res)

		case tokFunction:
			fn := strings.ToLower(tok.val)
			switch fn {
			case "sqrt":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for sqrt")
				}
				arg := valStack[len(valStack)-1]
				if arg < 0 {
					return 0, errors.New("sqrt of negative number requires complex numbers")
				}
				valStack[len(valStack)-1] = math.Sqrt(arg)
			case "cbrt":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for cbrt")
				}
				valStack[len(valStack)-1] = math.Cbrt(valStack[len(valStack)-1])
			case "abs":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for abs")
				}
				valStack[len(valStack)-1] = math.Abs(valStack[len(valStack)-1])
			case "ceil":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for ceil")
				}
				valStack[len(valStack)-1] = math.Ceil(valStack[len(valStack)-1])
			case "floor":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for floor")
				}
				valStack[len(valStack)-1] = math.Floor(valStack[len(valStack)-1])
			case "round":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for round")
				}
				valStack[len(valStack)-1] = math.Round(valStack[len(valStack)-1])
			case "sin":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for sin")
				}
				valStack[len(valStack)-1] = math.Sin(valStack[len(valStack)-1])
			case "cos":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for cos")
				}
				valStack[len(valStack)-1] = math.Cos(valStack[len(valStack)-1])
			case "tan":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for tan")
				}
				valStack[len(valStack)-1] = math.Tan(valStack[len(valStack)-1])
			case "asin":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for asin")
				}
				valStack[len(valStack)-1] = math.Asin(valStack[len(valStack)-1])
			case "acos":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for acos")
				}
				valStack[len(valStack)-1] = math.Acos(valStack[len(valStack)-1])
			case "atan":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for atan")
				}
				valStack[len(valStack)-1] = math.Atan(valStack[len(valStack)-1])
			case "log", "ln":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for log")
				}
				arg := valStack[len(valStack)-1]
				if arg <= 0 {
					return 0, errors.New("log of non-positive number is undefined")
				}
				valStack[len(valStack)-1] = math.Log(arg)
			case "log10":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for log10")
				}
				arg := valStack[len(valStack)-1]
				if arg <= 0 {
					return 0, errors.New("log10 of non-positive number is undefined")
				}
				valStack[len(valStack)-1] = math.Log10(arg)
			case "log2":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for log2")
				}
				arg := valStack[len(valStack)-1]
				if arg <= 0 {
					return 0, errors.New("log2 of non-positive number is undefined")
				}
				valStack[len(valStack)-1] = math.Log2(arg)
			case "exp":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for exp")
				}
				valStack[len(valStack)-1] = math.Exp(valStack[len(valStack)-1])
			case "min":
				if len(valStack) < 2 {
					return 0, errors.New("min function requires 2 arguments")
				}
				b := valStack[len(valStack)-1]
				a := valStack[len(valStack)-2]
				valStack = valStack[:len(valStack)-2]
				valStack = append(valStack, math.Min(a, b))
			case "max":
				if len(valStack) < 2 {
					return 0, errors.New("max function requires 2 arguments")
				}
				b := valStack[len(valStack)-1]
				a := valStack[len(valStack)-2]
				valStack = valStack[:len(valStack)-2]
				valStack = append(valStack, math.Max(a, b))
			case "pow":
				if len(valStack) < 2 {
					return 0, errors.New("pow function requires 2 arguments (base, exp)")
				}
				b := valStack[len(valStack)-1]
				a := valStack[len(valStack)-2]
				valStack = valStack[:len(valStack)-2]
				valStack = append(valStack, math.Pow(a, b))
			case "fact", "factorial":
				if len(valStack) < 1 {
					return 0, errors.New("missing argument for factorial")
				}
				n := valStack[len(valStack)-1]
				if n < 0 || math.Floor(n) != n {
					return 0, errors.New("factorial is only defined for non-negative integers")
				}
				valStack[len(valStack)-1] = factorial(int64(n))
			default:
				return 0, fmt.Errorf("unknown function %q", fn)
			}
		}
	}

	if len(valStack) != 1 {
		return 0, errors.New("invalid expression syntax")
	}

	return valStack[0], nil
}

func factorial(n int64) float64 {
	if n > 170 {
		return math.Inf(1)
	}
	var res float64 = 1
	for i := int64(2); i <= n; i++ {
		res *= float64(i)
	}
	return res
}
