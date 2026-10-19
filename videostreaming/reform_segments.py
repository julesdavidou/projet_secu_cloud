import httpx


def trigger_remote_reconstruction(nodes: list[str], video_id: str):
    """Ordonne à chaque nœud distant de reconstituer ses segments locaux à partir des blocs reçus."""
    print(" [+] Envoi de l'ordre de reconstitution aux nœuds distants...")

    with httpx.Client(timeout=60.0) as client:
        for node in nodes:
            url = f"{node}/reconstruct/{video_id}"
            try:
                response = client.post(url)
                if response.status_code == 200:
                    print(
                        f"  [+] Reconstitution réussie sur le nœud {node}"
                    )
                else:
                    print(
                        f"  [!] Erreur {response.status_code} sur {node}: {response.text}"
                    )
            except Exception as e:
                print(
                    f"  [!] Impossible de contacter le nœud {node} : {e}"
                )