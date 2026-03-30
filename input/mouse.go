package input

type MouseButton uint8

const (
	MouseButtonLeft   MouseButton = 0
	MouseButtonRight  MouseButton = 1
	MouseButtonMiddle MouseButton = 2
)

type MouseButtonAction uint8

const (
	ButtonRelease MouseButtonAction = 0
	ButtonPress   MouseButtonAction = 1
	ButtonRepeat  MouseButtonAction = 2
)

type MouseInput interface {
	GetPos() (x, y float64)
	GetButton(button MouseButton) MouseButtonAction
	GetScroll() (x, y float64)
}
