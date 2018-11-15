package main

import (
	"encoding/json"
	"net/http"
)

func createVtepHandler(w http.ResponseWriter, r *http.Request) {
	decoder := json.NewDecoder(r.Body)
	vtep := &vtepData{}
	if err := decoder.Decode(&vtep); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
	}
	defer r.Body.Close()

	if err := vtep.createOrUpdate(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error()))
	}

	w.WriteHeader(http.StatusOK)
}
