package emulator

// func (t *Terminal) Capture(ch chan bool) []string {
// 	done := make(chan bool, 1)
// 	var frames []string
//
// 	go func() {
// 		ticker := time.NewTicker(50 * time.Millisecond)
//
// 		for {
// 			select {
// 			case <-done:
// 				return
// 			case <-ticker.C:
// 				l := buf.Len()
// 				r := buf.Next(l)
//
// 				vt.Write(r)
//
// 				var frame bytes.Buffer
// 				vt.Render(&frame)
//
// 				frames = append(frames, frame.String())
// 			}
// 		}
// 	}()
// }
