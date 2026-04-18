// To implement controller, one needs to implement the interface below.
package controller

type ControllerInterface interface {
	Start() error
	Close() error
}
