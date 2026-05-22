package osrsge

import "math"

// taxExemptItemIDs lists items the Grand Exchange never taxes on sale,
// regardless of sale price.
//
// Source: OSRS Wiki, "Grand Exchange Tax" (https://oldschool.runescape.wiki/w/Grand_Exchange#Tax).
// The prices.runescape.wiki API exposes no exempt-item endpoint, so this list
// is maintained by hand. It covers the Old school bond plus the standard set
// of low-cost tools that the GE leaves untaxed.
var taxExemptItemIDs = map[int64]bool{
	13190: true, // Old school bond
	1755:  true, // Chisel
	5325:  true, // Gardening trowel
	11090: true, // Glassblowing pipe
	2347:  true, // Hammer
	1733:  true, // Needle
	233:   true, // Pestle and mortar
	5341:  true, // Rake
	8794:  true, // Saw
	5329:  true, // Secateurs
	5343:  true, // Seed dibber
	1735:  true, // Shears
	952:   true, // Spade
	5331:  true, // Watering can
}

// isTaxExempt reports whether the Grand Exchange waives sale tax for an item.
func isTaxExempt(itemID int64) bool {
	return taxExemptItemIDs[itemID]
}

// geTax computes the Grand Exchange sale tax for a single unit sold at
// sellPrice. Sales of 100 gp or less are untaxed; otherwise the tax is
// taxRate of the sale price, floored, and capped at taxCap per item.
func geTax(sellPrice int64, taxRate float64, taxCap int64) int64 {
	if sellPrice <= 100 || taxRate <= 0 {
		return 0
	}
	tax := int64(math.Floor(float64(sellPrice) * taxRate))
	if taxCap > 0 && tax > taxCap {
		return taxCap
	}
	return tax
}

// geTaxForItem applies geTax unless the item is on the exempt list, in which
// case the sale is untaxed at any price.
func geTaxForItem(itemID, sellPrice int64, taxRate float64, taxCap int64) int64 {
	if isTaxExempt(itemID) {
		return 0
	}
	return geTax(sellPrice, taxRate, taxCap)
}
