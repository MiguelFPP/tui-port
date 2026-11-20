package domain

import "errors"

// Sentinels wrapped by adapters with %w; cancellation uses context errors instead.
var (
	ErrInvalidArgument  = errors.New("argumento inválido")
	ErrNotFound         = errors.New("no encontrado")
	ErrPermissionDenied = errors.New("permiso denegado")
	ErrUnavailable      = errors.New("monitor no disponible")
	ErrUnsupported      = errors.New("sistema no soportado")
	ErrProcessProtected = errors.New("proceso protegido")
	ErrTooLarge         = errors.New("demasiados elementos")
)
