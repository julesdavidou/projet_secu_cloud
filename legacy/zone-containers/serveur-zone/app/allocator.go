/*
INSA HDF - 5A ICY - Sécurité Cloud
Projet CaribouTV

"allocator.go" gère le placement des fragments sur les nœuds
*/
package main

import (
	"errors"
	"fmt"
	"log"
)

func allocateStorage(totalSize int64, numSegments int) ([]AllocatedNode, error) {
	if numSegments <= 0 {
		return nil, errors.New("le nombre de fragments doit être supérieur à 0")
	}

	// Chaque fragment a exactement la même taille
	segmentSize := totalSize / int64(numSegments)

	// Filtrage des nœuds ONLINE ayant au moins l'espace pour 1 fragment
	eligibleNodes, err := getEligibleNodes(segmentSize)
	if err != nil || len(eligibleNodes) == 0 {
		log.Println("Allocation impossible : espace insuffisant sur les nœuds de la zone")
		return nil, errors.New("aucun nœud disponible avec l'espace requis")
	}

	var selectedNodes []AllocatedNode

	// Distribution des fragments sur les nœuds éligibles (Round-Robin)
	for i := 0; i < numSegments; i++ {
		targetNode := eligibleNodes[i%len(eligibleNodes)]

		selectedNodes = append(selectedNodes, AllocatedNode{
			NodeID:   targetNode.ID,
			Endpoint: fmt.Sprintf("https://%s:8443", targetNode.IP),
		})
	}

	return selectedNodes, nil
}
