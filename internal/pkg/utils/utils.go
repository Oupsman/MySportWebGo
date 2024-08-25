package utils

import (
	"MySportWeb/internal/pkg/vars"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/exp/constraints"
	"math"
	"strings"
)

type Number interface {
	constraints.Float | constraints.Integer
}

func GenerateHashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CompareHashPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func ParseToken(tokenString string) (claims jwt.MapClaims, err error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		return []byte(vars.SecretKey), nil
	})

	if err != nil {
		return nil, err
	}

	claims = token.Claims.(jwt.MapClaims)
	/*if !ok {
		return nil, err
	}*/

	return claims, nil
}

func GetUserID(tokenString string) (uint64, error) {
	reqToken := strings.Split(tokenString, " ")[1]

	claims, err := ParseToken(reqToken)
	if err != nil {
		return 0, err
	}
	return uint64(claims["sub"].(float64)), nil
}

func GetUserUUID(tokenString string) (uuid.UUID, error) {
	reqToken := strings.Split(tokenString, " ")[1]

	claims, err := ParseToken(reqToken)
	if err != nil {
		return uuid.UUID{}, err
	}
	return uuid.Parse(claims["uuid"].(string))
}

func StringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func GetUserRole(tokenString string) (string, error) {
	reqToken := strings.Split(tokenString, " ")[1]

	claims, err := ParseToken(reqToken)
	if err != nil {
		return "", err
	}
	return claims["role"].(string), nil
}

func SemiCircleToDegres(semi int32) float64 {
	if semi > 0 {
		return float64(semi) * (180.0 / math.Pow(2.0, 31.0))
	}
	return 0
}

func MmsToKmh(mms float64) float64 {
	return mms * 3600 / 1000000
}

func CmToKm(cm uint32) float64 {
	return float64(cm) / 100000
}

func ConvertTime(time uint32) string {
	result := ""
	hour := time / 3600
	minute := (time % 3600) / 60
	second := time % 60
	if hour < 10 {
		result += fmt.Sprintf("0%d", hour)

	} else {
		result += fmt.Sprintf("%d", hour)
	}
	result += ":"
	if minute < 10 {
		result += fmt.Sprintf("0%d", minute)
	} else {
		result += fmt.Sprintf("%d", minute)
	}
	result += ":"
	if second < 10 {
		result += fmt.Sprintf("0%d", second)
	} else {
		result += fmt.Sprintf("%d", second)
	}
	return result
}

func Avg[T Number](arr []T) float64 {

	var sum float64

	if len(arr) == 0 {
		return 0
	}

	for _, val := range arr {
		sum += float64(val)
	}
	return sum / float64(len(arr))
}

func Min(arr []float64) float64 {
	min := arr[0]
	for _, val := range arr {
		if val < min {
			min = val
		}
	}
	return min
}

func Max(arr []float64) float64 {
	max := arr[0]
	for _, val := range arr {
		if val > max {
			max = val
		}
	}
	return max
}

func GravityFactor(weight, slope float64) float64 {
	g := 9.80665
	return g * math.Sin(math.Atan(slope)) * weight
}

func RollingResistance(weight, slope float64) float64 {
	g := 9.80665
	crr := 0.005
	return g * math.Cos(math.Atan(slope)) * weight * crr
}

func AerodynamicDrag(altitude float64) float64 {
	return 0.5 * 0.324 * (1.225 * math.Exp(-0.0011856*altitude))
}

// SavitzkyGolay applies the Savitzky-Golay filter to smooth the data.
// coeffs are the filter coefficients, data is the input signal, and it returns the smoothed data.
func SavitzkyGolay(data []float64, windowSize int, polyOrder int) []float64 {
	if len(data) < windowSize {
		return data
	}

	halfWindow := (windowSize - 1) / 2
	coeffs := computeCoefficients(windowSize, polyOrder)
	smoothed := make([]float64, len(data))

	for i := range data {
		sum := 0.0
		for j := -halfWindow; j <= halfWindow; j++ {
			idx := i + j
			if idx < 0 {
				idx = 0
			} else if idx >= len(data) {
				idx = len(data) - 1
			}
			sum += coeffs[halfWindow+j] * data[idx]
		}
		smoothed[i] = sum
	}

	return smoothed
}

// computeCoefficients calculates the filter coefficients for a given window size and polynomial order.
func computeCoefficients(windowSize int, polyOrder int) []float64 {
	halfWindow := (windowSize - 1) / 2
	a := make([][]float64, windowSize)
	for i := range a {
		a[i] = make([]float64, polyOrder+1)
		x := float64(i - halfWindow)
		for j := 0; j <= polyOrder; j++ {
			a[i][j] = math.Pow(x, float64(j))
		}
	}

	// Transpose of matrix a
	at := transpose(a)
	ata := multiplyMatrices(at, a)
	ataInv := invertMatrix(ata)
	ataInvAt := multiplyMatrices(ataInv, at)

	coeffs := ataInvAt[halfWindow]
	return coeffs
}

// transpose returns the transpose of a matrix.
func transpose(a [][]float64) [][]float64 {
	rows := len(a)
	cols := len(a[0])
	at := make([][]float64, cols)
	for i := range at {
		at[i] = make([]float64, rows)
		for j := range at[i] {
			at[i][j] = a[j][i]
		}
	}
	return at
}

// multiplyMatrices multiplies two matrices.
func multiplyMatrices(a, b [][]float64) [][]float64 {
	rows := len(a)
	cols := len(b[0])
	bRows := len(b)
	result := make([][]float64, rows)
	for i := range result {
		result[i] = make([]float64, cols)
		for j := range result[i] {
			sum := 0.0
			for k := 0; k < bRows; k++ {
				sum += a[i][k] * b[k][j]
			}
			result[i][j] = sum
		}
	}
	return result
}

// invertMatrix inverts a square matrix using Gaussian elimination.
func invertMatrix(a [][]float64) [][]float64 {
	n := len(a)
	aug := make([][]float64, n)
	for i := range aug {
		aug[i] = make([]float64, 2*n)
		copy(aug[i], a[i])
		aug[i][i+n] = 1.0
	}

	for i := range aug {
		scale := aug[i][i]
		for j := range aug[i] {
			aug[i][j] /= scale
		}
		for k := range aug {
			if i != k {
				scale = aug[k][i]
				for j := range aug[k] {
					aug[k][j] -= scale * aug[i][j]
				}
			}
		}
	}

	inv := make([][]float64, n)
	for i := range inv {
		inv[i] = make([]float64, n)
		copy(inv[i], aug[i][n:])
	}

	return inv
}

func NormalizedAvg[T Number](arr []T) float64 {
	var sum float64

	if len(arr) == 0 {
		return 0
	}

	for _, val := range arr {
		sum += math.Pow(float64(val), 4)
	}
	return sum / float64(len(arr))
}
