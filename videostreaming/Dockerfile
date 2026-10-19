FROM python:3.12-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY blocks.py hls_utils.py sendBlocks.py reconstruction_contract.py segment_signer.py crypto_serialize.py \
     SISIntegrity.py IBSSignature.py edge_client_agent.py ./
RUN useradd -u 10001 -M worker
USER 10001:10001
EXPOSE 9200
CMD ["uvicorn", "edge_client_agent:app", "--host", "0.0.0.0", "--port", "9200"]
