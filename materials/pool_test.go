package materials

import (
	"github.com/bluescreen10/pix/shaders"
	"strings"
	"testing"
)

// TestRegisterRejectsWrongRecordSize covers register's guard: a store is keyed by
// shader and its stride comes from the first material registered, so a material whose
// Bytes are a different length would write over neighbouring slots.
func TestRegisterRejectsWrongRecordSize(t *testing.T) {
	store, _ := testStore(t)

	st := store.Pool(Shader{Forward: shaders.BasicForward}, "probe")
	st.Create(&BasicMaterial{}) // sets the store's stride to the Basic record size

	defer func() {
		got, ok := recover().(string)
		if !ok || !strings.Contains(got, "material record is") {
			t.Fatalf("want a record-size panic, got %v", got)
		}
	}()
	st.Create(&PBRMaterial{}) // a longer record; would overrun the neighbouring slot
	t.Fatal("register accepted a record of the wrong size")
}
