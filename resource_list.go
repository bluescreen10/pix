package pix

type ResourceUpdateFunc[T, R any] func(item T, resource R) (R, error)
type ResourceDeleteFunc[R any] func(resource R) error

type ResourceList[T, R any] struct {
	items         []T
	resource      []R
	free          []int
	pendingDelete map[int]struct{}
}

func (rl *ResourceList[T, R]) Init() {
	// Slot 0 is reserved for "null" resource
	rl.items = make([]T, 1, 4096)
	rl.resource = make([]R, 1, 4096)
	rl.pendingDelete = make(map[int]struct{})
}

func (rl *ResourceList[T, R]) Add(item T) int {
	if len(rl.free) > 0 {
		index := rl.free[len(rl.free)-1]
		rl.free = rl.free[:len(rl.free)-1]
		rl.items[index] = item
		return index
	} else {
		var zeroR R
		rl.items = append(rl.items, item)
		rl.resource = append(rl.resource, zeroR)
		return len(rl.items) - 1
	}
}

func (rl *ResourceList[T, R]) AllItems() []T {
	return rl.items
}

func (rl *ResourceList[T, R]) AllResources() []R {
	return rl.resource
}

func (rl *ResourceList[T, R]) Clear() {
	rl.items = rl.items[:0]
	rl.resource = rl.resource[:0]
	rl.free = rl.free[:0]
	clear(rl.pendingDelete)
}

func (rl *ResourceList[T, R]) Delete(index int) {
	rl.pendingDelete[index] = struct{}{}
}

func (rl *ResourceList[T, R]) Get(index int) T {
	return rl.items[index]
}

func (rl *ResourceList[T, R]) GetResource(index int) R {
	return rl.resource[index]
}

// FIXME: Hack for now
func (rl *ResourceList[T, R]) GetResourcePtr(index int) *R {
	return &rl.resource[index]
}

func (rl *ResourceList[T, R]) Set(index int, item T) {
	rl.items[index] = item
}

func (rl *ResourceList[T, R]) SetResource(index int, resource R) {
	rl.resource[index] = resource
}

func (rl *ResourceList[T, R]) ProcessDelete(fn ResourceDeleteFunc[R]) error {
	for index := range rl.pendingDelete {
		resource := rl.resource[index]
		err := fn(resource)
		if err != nil {
			return err
		}

		var zeroR R
		var zeroT T
		rl.resource[index] = zeroR
		rl.items[index] = zeroT
		rl.free = append(rl.free, index)
	}

	clear(rl.pendingDelete)
	return nil
}
