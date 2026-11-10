#!/bin/bash
# make.sh [--solved] DIR: builds the shop repo in DIR, a new git repo, with six
# seeded bugs, one per package, each caught by a failing test. --solved leaves
# the bugs out. In the sources below, a line ending in "// @fix" is the right
# code and a line ending in "// @bug" is the seeded bug; make.sh keeps one of
# the two and strips the marker, so the repo never shows either.
set -euo pipefail
solved=0
if [ "${1:-}" = --solved ]; then solved=1; shift; fi
dir=${1:?usage: make.sh [--solved] DIR}
[ ! -e "$dir" ] || { echo "make.sh: $dir exists" >&2; exit 2; }
mkdir -p "$dir"
cd "$dir"
mkdir -p money cart discount tax inventory receipt

cat > go.mod <<'EOF'
module example.com/shop

go 1.21
EOF

cat > README.md <<'EOF'
# shop

Money, carts, discounts, sales tax, inventory and receipts for a small shop.
Run the tests with `go test ./...`.
EOF

cat > money/money.go <<'EOF'
// Package money is amounts of money in cents.
package money

import (
	"fmt"
	"strconv"
	"strings"
)

// Cents is an amount of money in cents.
type Cents int64

// Parse reads an amount such as "12.34", "12.3", "12" or "-0.05".
func Parse(s string) (Cents, error) {
	neg := strings.HasPrefix(s, "-")
	body := strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(body, ".")
	if whole == "" || len(frac) > 2 {
		return 0, fmt.Errorf("money: bad amount %q", s)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: bad amount %q", s)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("money: bad amount %q", s)
	}
	c := Cents(w*100 + f)
	if neg {
		c = -c
	}
	return c, nil
}

// String formats c as "12.34", or "-0.05" for a negative amount.
func (c Cents) String() string {
	sign := "" // @fix
	if c < 0 { // @fix
		sign, c = "-", -c // @fix
	} // @fix
	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100) // @fix
	return fmt.Sprintf("%d.%02d", c/100, c%100) // @bug
}
EOF

cat > money/money_test.go <<'EOF'
package money

import "testing"

func TestParse(t *testing.T) {
	cases := map[string]Cents{"12.34": 1234, "12.3": 1230, "12": 1200, "0.05": 5, "-0.05": -5, "-12.34": -1234}
	for s, want := range cases {
		got, err := Parse(s)
		if err != nil || got != want {
			t.Errorf("Parse(%q) = %d, %v; want %d", s, got, err, want)
		}
	}
	for _, s := range []string{"", "-", "1.234", "abc", "1.2.3"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q): want an error", s)
		}
	}
}

func TestString(t *testing.T) {
	cases := map[Cents]string{0: "0.00", 5: "0.05", 1234: "12.34", 100: "1.00", -5: "-0.05", -1234: "-12.34"}
	for c, want := range cases {
		if got := c.String(); got != want {
			t.Errorf("Cents(%d).String() = %q, want %q", int64(c), got, want)
		}
	}
}
EOF

cat > cart/cart.go <<'EOF'
// Package cart holds the lines of an order.
package cart

import (
	"fmt"

	"example.com/shop/money"
)

// Line is one product in the cart.
type Line struct {
	Name  string
	Price money.Cents
	Qty   int
}

// Cart is an order being built. The zero value is an empty cart.
type Cart struct {
	lines []Line
}

// Add puts qty of a product in the cart. Adding a product that is already
// there adds to its quantity.
func (c *Cart) Add(name string, price money.Cents, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("cart: quantity %d for %s", qty, name)
	}
	for i := range c.lines {
		if c.lines[i].Name == name {
			c.lines[i].Qty += qty
			return nil
		}
	}
	c.lines = append(c.lines, Line{Name: name, Price: price, Qty: qty})
	return nil
}

// Lines returns a copy of the cart's lines in the order they were added.
func (c *Cart) Lines() []Line {
	return append([]Line(nil), c.lines...)
}

// Total is the sum of price times quantity over every line.
func (c *Cart) Total() money.Cents {
	var t money.Cents
	for i := 0; i < len(c.lines); i++ { // @fix
	for i := 1; i < len(c.lines); i++ { // @bug
		l := c.lines[i]
		t += l.Price * money.Cents(l.Qty)
	}
	return t
}
EOF

cat > cart/cart_test.go <<'EOF'
package cart

import "testing"

func TestAddMerges(t *testing.T) {
	var c Cart
	c.Add("pen", 150, 2)
	c.Add("pen", 150, 1)
	if ls := c.Lines(); len(ls) != 1 || ls[0].Qty != 3 {
		t.Fatalf("Lines = %+v, want one pen line with Qty 3", ls)
	}
	if err := c.Add("pad", 300, 0); err == nil {
		t.Fatal("Add with quantity 0: want an error")
	}
}

func TestTotal(t *testing.T) {
	var c Cart
	c.Add("pen", 150, 2)
	c.Add("pad", 300, 1)
	c.Add("ink", 25, 4)
	if got := c.Total(); got != 700 {
		t.Fatalf("Total = %d, want 700", got)
	}
}
EOF

cat > discount/discount.go <<'EOF'
// Package discount works out what an order saves.
package discount

import "example.com/shop/money"

// PercentOff returns pct percent of amount, rounded down to the cent. pct is
// clamped to 0..100.
func PercentOff(amount money.Cents, pct int) money.Cents {
	if pct <= 0 {
		return 0
	}
	if pct > 100 {
		pct = 100
	}
	return amount * money.Cents(pct) / 100
}

// BuyXGetY returns how many of qty items are free under "buy x, get y free":
// every full group of x+y items has y of them free.
func BuyXGetY(qty, x, y int) int {
	if x <= 0 || y <= 0 || qty <= 0 {
		return 0
	}
	return qty / (x + y) * y // @fix
	return qty / x * y // @bug
}
EOF

cat > discount/discount_test.go <<'EOF'
package discount

import "testing"

func TestPercentOff(t *testing.T) {
	if got := PercentOff(1999, 10); got != 199 {
		t.Errorf("PercentOff(1999, 10) = %d, want 199", got)
	}
	if got := PercentOff(500, 150); got != 500 {
		t.Errorf("PercentOff(500, 150) = %d, want 500", got)
	}
	if got := PercentOff(500, -5); got != 0 {
		t.Errorf("PercentOff(500, -5) = %d, want 0", got)
	}
}

func TestBuyXGetY(t *testing.T) {
	cases := []struct{ qty, x, y, want int }{
		{3, 2, 1, 1},
		{5, 2, 1, 1},
		{6, 2, 1, 2},
		{2, 2, 1, 0},
		{8, 3, 1, 2},
		{7, 1, 1, 3},
		{0, 2, 1, 0},
	}
	for _, c := range cases {
		if got := BuyXGetY(c.qty, c.x, c.y); got != c.want {
			t.Errorf("BuyXGetY(%d, %d, %d) = %d, want %d", c.qty, c.x, c.y, got, c.want)
		}
	}
}
EOF

cat > tax/tax.go <<'EOF'
// Package tax works out sales tax.
package tax

import (
	"fmt"
	"math"

	"example.com/shop/money"
)

var rates = map[string]float64{
	"CA": 0.0725,
	"NY": 0.04,
	"OR": 0,
	"TX": 0.0625,
}

// Rate returns the tax rate for a region code such as "CA".
func Rate(region string) (float64, error) {
	r, ok := rates[region]
	if !ok {
		return 0, fmt.Errorf("tax: unknown region %q", region)
	}
	return r, nil
}

// Tax returns the tax on amount at rate, rounded to the nearest cent, with
// halves rounded away from zero.
func Tax(amount money.Cents, rate float64) money.Cents {
	return money.Cents(math.Round(float64(amount) * rate)) // @fix
	return money.Cents(math.Floor(float64(amount) * rate)) // @bug
}
EOF

cat > tax/tax_test.go <<'EOF'
package tax

import (
	"testing"

	"example.com/shop/money"
)

func TestRate(t *testing.T) {
	if r, err := Rate("CA"); err != nil || r != 0.0725 {
		t.Errorf("Rate(CA) = %v, %v", r, err)
	}
	if _, err := Rate("ZZ"); err == nil {
		t.Error("Rate(ZZ): want an error")
	}
}

func TestTax(t *testing.T) {
	cases := []struct {
		amount int64
		rate   float64
		want   int64
	}{
		{1999, 0.0725, 145},
		{1000, 0.0625, 63},
		{1000, 0.04, 40},
		{999, 0, 0},
		{150, 0.0725, 11},
	}
	for _, c := range cases {
		if got := Tax(money.Cents(c.amount), c.rate); int64(got) != c.want {
			t.Errorf("Tax(%d, %v) = %d, want %d", c.amount, c.rate, int64(got), c.want)
		}
	}
}
EOF

cat > inventory/inventory.go <<'EOF'
// Package inventory tracks stock and reservations.
package inventory

import "fmt"

type stock struct {
	onHand, reserved int
}

// Inventory is stock per SKU.
type Inventory struct {
	stock map[string]stock
}

// New returns an empty inventory.
func New() *Inventory { return &Inventory{stock: map[string]stock{}} }

// Receive adds qty units of sku to the stock on hand.
func (inv *Inventory) Receive(sku string, qty int) {
	s := inv.stock[sku]
	s.onHand += qty
	inv.stock[sku] = s
}

// Available is the stock on hand that is not reserved.
func (inv *Inventory) Available(sku string) int {
	s := inv.stock[sku]
	return s.onHand - s.reserved
}

// Reserve holds qty units for an order. It fails when fewer are available.
func (inv *Inventory) Reserve(sku string, qty int) error {
	if qty <= 0 {
		return fmt.Errorf("inventory: reserving %d", qty)
	}
	if avail := inv.Available(sku); qty > avail {
		return fmt.Errorf("inventory: reserving %d of %s, only %d available", qty, sku, avail)
	}
	s := inv.stock[sku]
	s.reserved += qty
	inv.stock[sku] = s
	return nil
}

// Release gives back qty units of a reservation, which become available again.
func (inv *Inventory) Release(sku string, qty int) error {
	s, ok := inv.stock[sku]
	if !ok {
		return fmt.Errorf("inventory: unknown sku %q", sku)
	}
	if qty > s.reserved {
		return fmt.Errorf("inventory: releasing %d of %s, only %d reserved", qty, sku, s.reserved)
	}
	s.reserved -= qty // @fix
	s.onHand -= qty // @bug
	inv.stock[sku] = s
	return nil
}

// Ship sends qty reserved units: they leave both the stock on hand and the
// reservation.
func (inv *Inventory) Ship(sku string, qty int) error {
	s := inv.stock[sku]
	if qty > s.reserved {
		return fmt.Errorf("inventory: shipping %d of %s, only %d reserved", qty, sku, s.reserved)
	}
	s.reserved -= qty
	s.onHand -= qty
	inv.stock[sku] = s
	return nil
}
EOF

cat > inventory/inventory_test.go <<'EOF'
package inventory

import "testing"

func TestReserve(t *testing.T) {
	inv := New()
	inv.Receive("pen", 10)
	if err := inv.Reserve("pen", 4); err != nil {
		t.Fatal(err)
	}
	if got := inv.Available("pen"); got != 6 {
		t.Fatalf("Available = %d, want 6", got)
	}
	if err := inv.Reserve("pen", 7); err == nil {
		t.Fatal("Reserve(7) with 6 available: want an error")
	}
}

func TestRelease(t *testing.T) {
	inv := New()
	inv.Receive("pen", 10)
	inv.Reserve("pen", 3)
	if err := inv.Release("pen", 3); err != nil {
		t.Fatal(err)
	}
	if got := inv.Available("pen"); got != 10 {
		t.Fatalf("Available after the release = %d, want 10", got)
	}
	if err := inv.Release("pen", 1); err == nil {
		t.Fatal("Release with nothing reserved: want an error")
	}
}

func TestShip(t *testing.T) {
	inv := New()
	inv.Receive("pen", 5)
	inv.Reserve("pen", 2)
	if err := inv.Ship("pen", 2); err != nil {
		t.Fatal(err)
	}
	if got := inv.Available("pen"); got != 3 {
		t.Fatalf("Available after shipping = %d, want 3", got)
	}
}
EOF

cat > receipt/receipt.go <<'EOF'
// Package receipt prints an order's receipt.
package receipt

import (
	"fmt"
	"sort"
	"strings"

	"example.com/shop/money"
)

// Item is one line of a receipt.
type Item struct {
	Name   string
	Amount money.Cents
}

// Format prints the items sorted by name, one per line, the name left-aligned
// and the amount right-aligned in 30 columns, then a TOTAL line. It does not
// reorder its input.
func Format(items []Item) string {
	lines := append([]Item(nil), items...)
	sort.Slice(lines, func(i, j int) bool { return lines[i].Name < lines[j].Name }) // @fix
	sort.Slice(lines, func(i, j int) bool { return lines[i].Name > lines[j].Name }) // @bug
	var b strings.Builder
	var total money.Cents
	for _, it := range lines {
		b.WriteString(row(it.Name, it.Amount))
		total += it.Amount
	}
	b.WriteString(row("TOTAL", total))
	return b.String()
}

func row(name string, amount money.Cents) string {
	a := amount.String()
	pad := 30 - len(name) - len(a)
	if pad < 1 {
		pad = 1
	}
	return fmt.Sprintf("%s%s%s\n", name, strings.Repeat(" ", pad), a)
}
EOF

cat > receipt/receipt_test.go <<'EOF'
package receipt

import (
	"strings"
	"testing"
)

func r(name, amount string) string {
	return name + strings.Repeat(" ", 30-len(name)-len(amount)) + amount + "\n"
}

func TestFormat(t *testing.T) {
	got := Format([]Item{{"pen", 300}, {"ink", 100}, {"pad", 300}})
	want := r("ink", "1.00") + r("pad", "3.00") + r("pen", "3.00") + r("TOTAL", "7.00")
	if got != want {
		t.Fatalf("Format =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatKeepsInput(t *testing.T) {
	in := []Item{{"b", 1}, {"a", 2}}
	Format(in)
	if in[0].Name != "b" {
		t.Fatal("Format reordered its input")
	}
}
EOF

if [ $solved = 1 ]; then
	keep='\@fix' drop='\@bug'
else
	keep='\@bug' drop='\@fix'
fi
for f in $(find . -name '*.go'); do
	perl -ni -e "next if m{ // $drop\$}; s{ // $keep\$}{}; print" "$f"
done
if grep -rn '// @' --include='*.go' . >&2; then
	echo "make.sh: a marker is left" >&2
	exit 1
fi

git init -q -b main
git add -A
git -c user.name=bench -c user.email=bench@example.com commit -q -m "shop: money, cart, discount, tax, inventory, receipt"
