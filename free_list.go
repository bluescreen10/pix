package pix

type FreeList[T any] struct {
	items []T
	free  []int
}

func (fl *FreeList[T]) Add(item T) int {
	if len(fl.free) > 0 {
		index := fl.free[len(fl.free)-1]
		fl.free = fl.free[:len(fl.free)-1]
		fl.items[index] = item
		return index
	} else {
		fl.items = append(fl.items, item)
		return len(fl.items) - 1
	}
}

func (fl *FreeList[T]) Delete(index int) {
	fl.items[index] = *new(T)
	fl.free = append(fl.free, index)
}

func (fl *FreeList[T]) Get(index int) T {
	return fl.items[index]
}

func (fl *FreeList[T]) Set(index int, item T) {
	fl.items[index] = item
}
