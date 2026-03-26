// Package pdf — encoding.go provides standard PDF encoding tables
// (WinAnsiEncoding, MacRomanEncoding) and the Adobe Glyph List mapping
// for Cyrillic and basic Latin glyph names to Unicode code points.
package pdf

// winAnsiEncoding maps byte values 0-255 to Unicode code points
// following the Windows-1252 / CP1252 specification.
// Positions 0-127 are standard ASCII. Positions 128-159 use the CP1252
// special characters. Positions 160-255 match Latin-1 Supplement.
var winAnsiEncoding [256]rune

// macRomanEncoding maps byte values 0-255 to Unicode code points
// following the Mac OS Roman encoding.
var macRomanEncoding [256]rune

// glyphToUnicode maps Adobe Glyph List names to Unicode runes.
// Covers basic Latin, punctuation, and Cyrillic characters (afii10017-afii10145).
var glyphToUnicode map[string]rune

func init() {
	// --- WinAnsiEncoding (CP1252) ---
	// 0-127: standard ASCII
	for i := 0; i < 128; i++ {
		winAnsiEncoding[i] = rune(i)
	}
	// 128-159: CP1252 special characters
	cp1252Upper := [32]rune{
		0x20AC, // 128: Euro sign €
		0xFFFD, // 129: undefined → replacement
		0x201A, // 130: Single low-9 quotation mark ‚
		0x0192, // 131: Latin small letter f with hook ƒ
		0x201E, // 132: Double low-9 quotation mark „
		0x2026, // 133: Horizontal ellipsis …
		0x2020, // 134: Dagger †
		0x2021, // 135: Double dagger ‡
		0x02C6, // 136: Modifier letter circumflex accent ˆ
		0x2030, // 137: Per mille sign ‰
		0x0160, // 138: Latin capital letter S with caron Š
		0x2039, // 139: Single left-pointing angle quotation mark ‹
		0x0152, // 140: Latin capital ligature OE Œ
		0xFFFD, // 141: undefined
		0x017D, // 142: Latin capital letter Z with caron Ž
		0xFFFD, // 143: undefined
		0xFFFD, // 144: undefined
		0x2018, // 145: Left single quotation mark '
		0x2019, // 146: Right single quotation mark '
		0x201C, // 147: Left double quotation mark "
		0x201D, // 148: Right double quotation mark "
		0x2022, // 149: Bullet •
		0x2013, // 150: En dash –
		0x2014, // 151: Em dash —
		0x02DC, // 152: Small tilde ˜
		0x2122, // 153: Trade mark sign ™
		0x0161, // 154: Latin small letter s with caron š
		0x203A, // 155: Single right-pointing angle quotation mark ›
		0x0153, // 156: Latin small ligature oe œ
		0xFFFD, // 157: undefined
		0x017E, // 158: Latin small letter z with caron ž
		0x0178, // 159: Latin capital letter Y with diaeresis Ÿ
	}
	for i, r := range cp1252Upper {
		winAnsiEncoding[128+i] = r
	}
	// 160-255: Latin-1 Supplement (same as Unicode)
	for i := 160; i <= 255; i++ {
		winAnsiEncoding[i] = rune(i)
	}

	// --- MacRomanEncoding ---
	// 0-127: standard ASCII
	for i := 0; i < 128; i++ {
		macRomanEncoding[i] = rune(i)
	}
	// 128-255: Mac OS Roman upper half
	macRomanUpper := [128]rune{
		0x00C4, // 128: Ä
		0x00C5, // 129: Å
		0x00C7, // 130: Ç
		0x00C9, // 131: É
		0x00D1, // 132: Ñ
		0x00D6, // 133: Ö
		0x00DC, // 134: Ü
		0x00E1, // 135: á
		0x00E0, // 136: à
		0x00E2, // 137: â
		0x00E4, // 138: ä
		0x00E3, // 139: ã
		0x00E5, // 140: å
		0x00E7, // 141: ç
		0x00E9, // 142: é
		0x00E8, // 143: è
		0x00EA, // 144: ê
		0x00EB, // 145: ë
		0x00ED, // 146: í
		0x00EC, // 147: ì
		0x00EE, // 148: î
		0x00EF, // 149: ï
		0x00F1, // 150: ñ
		0x00F3, // 151: ó
		0x00F2, // 152: ò
		0x00F4, // 153: ô
		0x00F6, // 154: ö
		0x00F5, // 155: õ
		0x00FA, // 156: ú
		0x00F9, // 157: ù
		0x00FB, // 158: û
		0x00FC, // 159: ü
		0x2020, // 160: †
		0x00B0, // 161: °
		0x00A2, // 162: ¢
		0x00A3, // 163: £
		0x00A7, // 164: §
		0x2022, // 165: •
		0x00B6, // 166: ¶
		0x00DF, // 167: ß
		0x00AE, // 168: ®
		0x00A9, // 169: ©
		0x2122, // 170: ™
		0x00B4, // 171: ´
		0x00A8, // 172: ¨
		0x2260, // 173: ≠
		0x00C6, // 174: Æ
		0x00D8, // 175: Ø
		0x221E, // 176: ∞
		0x00B1, // 177: ±
		0x2264, // 178: ≤
		0x2265, // 179: ≥
		0x00A5, // 180: ¥
		0x00B5, // 181: µ
		0x2202, // 182: ∂
		0x2211, // 183: ∑
		0x220F, // 184: ∏
		0x03C0, // 185: π
		0x222B, // 186: ∫
		0x00AA, // 187: ª
		0x00BA, // 188: º
		0x2126, // 189: Ω
		0x00E6, // 190: æ
		0x00F8, // 191: ø
		0x00BF, // 192: ¿
		0x00A1, // 193: ¡
		0x00AC, // 194: ¬
		0x221A, // 195: √
		0x0192, // 196: ƒ
		0x2248, // 197: ≈
		0x2206, // 198: ∆
		0x00AB, // 199: «
		0x00BB, // 200: »
		0x2026, // 201: …
		0x00A0, // 202: non-breaking space
		0x00C0, // 203: À
		0x00C3, // 204: Ã
		0x00D5, // 205: Õ
		0x0152, // 206: Œ
		0x0153, // 207: œ
		0x2013, // 208: –
		0x2014, // 209: —
		0x201C, // 210: "
		0x201D, // 211: "
		0x2018, // 212: '
		0x2019, // 213: '
		0x00F7, // 214: ÷
		0x25CA, // 215: ◊
		0x00FF, // 216: ÿ
		0x0178, // 217: Ÿ
		0x2044, // 218: ⁄
		0x20AC, // 219: €
		0x2039, // 220: ‹
		0x203A, // 221: ›
		0xFB01, // 222: fi
		0xFB02, // 223: fl
		0x2021, // 224: ‡
		0x00B7, // 225: ·
		0x201A, // 226: ‚
		0x201E, // 227: „
		0x2030, // 228: ‰
		0x00C2, // 229: Â
		0x00CA, // 230: Ê
		0x00C1, // 231: Á
		0x00CB, // 232: Ë
		0x00C8, // 233: È
		0x00CD, // 234: Í
		0x00CE, // 235: Î
		0x00CF, // 236: Ï
		0x00CC, // 237: Ì
		0x00D3, // 238: Ó
		0x00D4, // 239: Ô
		0xF8FF, // 240: Apple logo (private use)
		0x00D2, // 241: Ò
		0x00DA, // 242: Ú
		0x00DB, // 243: Û
		0x00D9, // 244: Ù
		0x0131, // 245: ı
		0x02C6, // 246: ˆ
		0x02DC, // 247: ˜
		0x00AF, // 248: ¯
		0x02D8, // 249: ˘
		0x02D9, // 250: ˙
		0x02DA, // 251: ˚
		0x00B8, // 252: ¸
		0x02DD, // 253: ˝
		0x02DB, // 254: ˛
		0x02C7, // 255: ˇ
	}
	for i, r := range macRomanUpper {
		macRomanEncoding[128+i] = r
	}

	// --- Adobe Glyph List (subset) ---
	glyphToUnicode = map[string]rune{
		// Basic Latin letters
		"A": 0x0041, "B": 0x0042, "C": 0x0043, "D": 0x0044,
		"E": 0x0045, "F": 0x0046, "G": 0x0047, "H": 0x0048,
		"I": 0x0049, "J": 0x004A, "K": 0x004B, "L": 0x004C,
		"M": 0x004D, "N": 0x004E, "O": 0x004F, "P": 0x0050,
		"Q": 0x0051, "R": 0x0052, "S": 0x0053, "T": 0x0054,
		"U": 0x0055, "V": 0x0056, "W": 0x0057, "X": 0x0058,
		"Y": 0x0059, "Z": 0x005A,
		"a": 0x0061, "b": 0x0062, "c": 0x0063, "d": 0x0064,
		"e": 0x0065, "f": 0x0066, "g": 0x0067, "h": 0x0068,
		"i": 0x0069, "j": 0x006A, "k": 0x006B, "l": 0x006C,
		"m": 0x006D, "n": 0x006E, "o": 0x006F, "p": 0x0070,
		"q": 0x0071, "r": 0x0072, "s": 0x0073, "t": 0x0074,
		"u": 0x0075, "v": 0x0076, "w": 0x0077, "x": 0x0078,
		"y": 0x0079, "z": 0x007A,

		// Digits
		"zero": 0x0030, "one": 0x0031, "two": 0x0032, "three": 0x0033,
		"four": 0x0034, "five": 0x0035, "six": 0x0036, "seven": 0x0037,
		"eight": 0x0038, "nine": 0x0039,

		// Common punctuation and symbols
		"space":        0x0020,
		"exclam":       0x0021,
		"quotedbl":     0x0022,
		"numbersign":   0x0023,
		"dollar":       0x0024,
		"percent":      0x0025,
		"ampersand":    0x0026,
		"quotesingle":  0x0027,
		"parenleft":    0x0028,
		"parenright":   0x0029,
		"asterisk":     0x002A,
		"plus":         0x002B,
		"comma":        0x002C,
		"hyphen":       0x002D,
		"period":       0x002E,
		"slash":        0x002F,
		"colon":        0x003A,
		"semicolon":    0x003B,
		"less":         0x003C,
		"equal":        0x003D,
		"greater":      0x003E,
		"question":     0x003F,
		"at":           0x0040,
		"bracketleft":  0x005B,
		"backslash":    0x005C,
		"bracketright": 0x005D,
		"asciicircum":  0x005E,
		"underscore":   0x005F,
		"grave":        0x0060,
		"braceleft":    0x007B,
		"bar":          0x007C,
		"braceright":   0x007D,
		"asciitilde":   0x007E,

		// Extended Latin
		"bullet":       0x2022,
		"endash":       0x2013,
		"emdash":       0x2014,
		"ellipsis":     0x2026,
		"quoteleft":    0x2018,
		"quoteright":   0x2019,
		"quotedblleft": 0x201C,
		"quotedblright": 0x201D,
		"guillemotleft":  0x00AB,
		"guillemotright": 0x00BB,
		"dagger":       0x2020,
		"daggerdbl":    0x2021,
		"perthousand":  0x2030,
		"trademark":    0x2122,
		"copyright":    0x00A9,
		"registered":   0x00AE,
		"degree":       0x00B0,
		"plusminus":     0x00B1,
		"multiply":      0x00D7,
		"divide":        0x00F7,
		"Euro":          0x20AC,
		"section":       0x00A7,
		"paragraph":     0x00B6,
		"nbspace":       0x00A0,
		"fi":            0xFB01,
		"fl":            0xFB02,

		// Cyrillic uppercase А-Я (afii10017-afii10048)
		// Mapping follows the widely-used convention in Russian PDF generators
		// (MS Word, LibreOffice) where afii10017-afii10048 = А(U+0410)-Я(U+042F)
		// and afii10049-afii10080 = а(U+0430)-я(U+044F) sequentially.
		"afii10017": 0x0410, // А
		"afii10018": 0x0411, // Б
		"afii10019": 0x0412, // В
		"afii10020": 0x0413, // Г
		"afii10021": 0x0414, // Д
		"afii10022": 0x0415, // Е
		"afii10023": 0x0416, // Ж
		"afii10024": 0x0417, // З
		"afii10025": 0x0418, // И
		"afii10026": 0x0419, // Й
		"afii10027": 0x041A, // К
		"afii10028": 0x041B, // Л
		"afii10029": 0x041C, // М
		"afii10030": 0x041D, // Н
		"afii10031": 0x041E, // О
		"afii10032": 0x041F, // П
		"afii10033": 0x0420, // Р
		"afii10034": 0x0421, // С
		"afii10035": 0x0422, // Т
		"afii10036": 0x0423, // У
		"afii10037": 0x0424, // Ф
		"afii10038": 0x0425, // Х
		"afii10039": 0x0426, // Ц
		"afii10040": 0x0427, // Ч
		"afii10041": 0x0428, // Ш
		"afii10042": 0x0429, // Щ
		"afii10043": 0x042A, // Ъ
		"afii10044": 0x042B, // Ы
		"afii10045": 0x042C, // Ь
		"afii10046": 0x042D, // Э
		"afii10047": 0x042E, // Ю
		"afii10048": 0x042F, // Я

		// Cyrillic lowercase а-я (afii10049-afii10080)
		"afii10049": 0x0430, // а
		"afii10050": 0x0431, // б
		"afii10051": 0x0432, // в
		"afii10052": 0x0433, // г
		"afii10053": 0x0434, // д
		"afii10054": 0x0435, // е
		"afii10055": 0x0436, // ж
		"afii10056": 0x0437, // з
		"afii10057": 0x0438, // и
		"afii10058": 0x0439, // й
		"afii10059": 0x043A, // к
		"afii10060": 0x043B, // л
		"afii10061": 0x043C, // м
		"afii10062": 0x043D, // н
		"afii10063": 0x043E, // о
		"afii10064": 0x043F, // п
		"afii10065": 0x0440, // р
		"afii10066": 0x0441, // с
		"afii10067": 0x0442, // т
		"afii10068": 0x0443, // у
		"afii10069": 0x0444, // ф
		"afii10070": 0x0445, // х
		"afii10071": 0x0446, // ц
		"afii10072": 0x0447, // ч
		"afii10073": 0x0448, // ш
		"afii10074": 0x0449, // щ
		"afii10075": 0x044A, // ъ
		"afii10076": 0x044B, // ы
		"afii10077": 0x044C, // ь
		"afii10078": 0x044D, // э
		"afii10079": 0x044E, // ю
		"afii10080": 0x044F, // я

		// Extended Cyrillic (Serbian, Ukrainian, etc.)
		"afii10081":  0x0402, // Ђ (Serbian)
		"afii10082":  0x0403, // Ѓ
		"afii10083":  0x0404, // Є
		"afii10084":  0x0405, // Ѕ
		"afii10085":  0x0406, // І
		"afii10086":  0x0407, // Ї
		"afii10087":  0x0408, // Ј
		"afii10088":  0x0409, // Љ
		"afii10089":  0x040A, // Њ
		"afii10090":  0x040B, // Ћ
		"afii10091":  0x040C, // Ќ
		"afii10092":  0x040D, // Ѝ
		"afii10093":  0x040E, // Ў
		"afii10094":  0x040F, // Џ

		"afii10095":  0x0452, // ђ
		"afii10096":  0x0453, // ѓ
		"afii10097":  0x0454, // є
		"afii10098":  0x0455, // ѕ
		"afii10099":  0x0456, // і
		"afii10100":  0x0457, // ї
		"afii10101":  0x0458, // ј
		"afii10102":  0x0459, // љ
		"afii10103":  0x045A, // њ
		"afii10104":  0x045B, // ћ
		"afii10105":  0x045C, // ќ
		"afii10106":  0x045D, // ѝ
		"afii10107":  0x045E, // ў
		"afii10108":  0x045F, // џ

		// Ukrainian Ґ/ґ
		"afii10109": 0x0490, // Ґ
		"afii10110": 0x0491, // ґ

		// Ё and ё — standard Adobe glyph names
		// Note: afii10023 maps to Ж(U+0416) per canonical AGL, not Ё(U+0401).
		// Ё/ё are available via "Iocyrillic"/"iocyrillic" and "uni0401"/"uni0451".
		"Iocyrillic":  0x0401, // Ё
		"iocyrillic":  0x0451, // ё

		// Standard number forms
		"numero": 0x2116, // №
	}

	// Common "uniXXXX" format names for Ё and ё
	glyphToUnicode["uni0401"] = 0x0401 // Ё
	glyphToUnicode["uni0451"] = 0x0451 // ё
}

// encodingTable returns a pointer to the named encoding table,
// or nil if the name is not recognized.
// Recognized names: "WinAnsiEncoding", "MacRomanEncoding".
func encodingTable(name string) *[256]rune {
	switch name {
	case "WinAnsiEncoding":
		return &winAnsiEncoding
	case "MacRomanEncoding":
		return &macRomanEncoding
	default:
		return nil
	}
}

// parseDifferencesArray parses a PDF Encoding /Differences array into a
// byte-to-rune mapping. The array format is:
//
//	[code /glyphName /glyphName ... code /glyphName ...]
//
// where code is an integer giving the starting byte position,
// and subsequent glyph names increment from that position.
// Unknown glyph names are silently skipped.
func parseDifferencesArray(arr []interface{}) map[byte]rune {
	result := make(map[byte]rune)
	var code int
	haveCode := false

	for _, item := range arr {
		switch v := item.(type) {
		case int:
			code = v
			haveCode = true
		case float64:
			code = int(v)
			haveCode = true
		case string:
			if !haveCode {
				continue
			}
			// Strip leading "/" if present (some callers pass raw names)
			name := v
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
			if r, ok := glyphToUnicode[name]; ok && code >= 0 && code <= 255 {
				result[byte(code)] = r
			} else if len(name) > 3 && name[:3] == "uni" {
				// Try uniXXXX format
				if r := parseUniName(name); r != 0 {
					if code >= 0 && code <= 255 {
						result[byte(code)] = r
					}
				}
			}
			code++
		}
	}

	return result
}

// parseUniName parses a "uniXXXX" glyph name into a rune.
// Returns 0 if parsing fails.
func parseUniName(name string) rune {
	if len(name) < 7 || name[:3] != "uni" {
		return 0
	}
	hex := name[3:]
	if len(hex) != 4 {
		return 0
	}
	var val rune
	for _, c := range hex {
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val |= c - '0'
		case c >= 'A' && c <= 'F':
			val |= c - 'A' + 10
		case c >= 'a' && c <= 'f':
			val |= c - 'a' + 10
		default:
			return 0
		}
	}
	return val
}
