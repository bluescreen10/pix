package input

type Key int

const (
	KeyUnknown      Key = -1
	KeySpace        Key = 32
	KeyApostrophe   Key = 39
	KeyComma        Key = 44
	KeyMinus        Key = 45
	KeyPeriod       Key = 46
	KeySlash        Key = 47
	Key0            Key = 48
	Key1            Key = 49
	Key2            Key = 50
	Key3            Key = 51
	Key4            Key = 52
	Key5            Key = 53
	Key6            Key = 54
	Key7            Key = 55
	Key8            Key = 56
	Key9            Key = 57
	KeySemicolon    Key = 59
	KeyEqual        Key = 61
	KeyA            Key = 65
	KeyB            Key = 66
	KeyC            Key = 67
	KeyD            Key = 68
	KeyE            Key = 69
	KeyF            Key = 70
	KeyG            Key = 71
	KeyH            Key = 72
	KeyI            Key = 73
	KeyJ            Key = 74
	KeyK            Key = 75
	KeyL            Key = 76
	KeyM            Key = 77
	KeyN            Key = 78
	KeyO            Key = 79
	KeyP            Key = 80
	KeyQ            Key = 81
	KeyR            Key = 82
	KeyS            Key = 83
	KeyT            Key = 84
	KeyU            Key = 85
	KeyV            Key = 86
	KeyW            Key = 87
	KeyX            Key = 88
	KeyY            Key = 89
	KeyZ            Key = 90
	KeyLeftBracket  Key = 91
	KeyBacklash     Key = 92
	KeyRightBracket Key = 93
	KeyGraveAccent  Key = 96
	KeyWorld1       Key = 161
	KeyWorld2       Key = 162

	/* Function keys=  */
	KeyEscape       = 256
	KeyEnter        = 257
	KeyTab          = 258
	KeyBackspace    = 259
	KeyInsert       = 260
	KeyDelete       = 261
	KeyRight        = 262
	KeyLeft         = 263
	KeyDown         = 264
	KeyUp           = 265
	KeyPageUp       = 266
	KeyPageDown     = 267
	KeyHome         = 268
	KeyEnd          = 269
	KeyCapsLock     = 280
	KeyScrollLock   = 281
	KeyNumLock      = 282
	KeyPrintScreen  = 283
	KeyPause        = 284
	KeyF1           = 290
	KeyF2           = 291
	KeyF3           = 292
	KeyF4           = 293
	KeyF5           = 294
	KeyF6           = 295
	KeyF7           = 296
	KeyF8           = 297
	KeyF9           = 298
	KeyF10          = 299
	KeyF11          = 300
	KeyF12          = 301
	KeyF13          = 302
	KeyF14          = 303
	KeyF15          = 304
	KeyF16          = 305
	KeyF17          = 306
	KeyF18          = 307
	KeyF19          = 308
	KeyF20          = 309
	KeyF21          = 310
	KeyF22          = 311
	KeyF23          = 312
	KeyF24          = 313
	KeyF25          = 314
	KeyKP0          = 320
	KeyKP1          = 321
	KeyKP2          = 322
	KeyKP3          = 323
	KeyKP4          = 324
	KeyKP5          = 325
	KeyKP6          = 326
	KeyKP7          = 327
	KeyKP8          = 328
	KeyKP9          = 329
	KeyKPDecimal    = 330
	KeyKPDivide     = 331
	KeyKPMultiply   = 332
	KeyKPSubtract   = 333
	KeyKPAdd        = 334
	KeyKPEnter      = 335
	KeyKPEqual      = 336
	KeyLeftShift    = 340
	KeyLeftControl  = 341
	KeyLeftAlt      = 342
	KeyLeftSuper    = 343
	KeyRightShift   = 344
	KeyRightControl = 345
	KeyRightAlt     = 346
	KeyRightSuper   = 347
	KeyMenu         = 348
)

type KeyAction uint8

const (
	KeyRelease KeyAction = 0
	KeyPress   KeyAction = 1
	KeyRepeat  KeyAction = 2
)

type KeyBoardInput interface {
	GetKey(key Key) KeyAction
}
