import heapq
n,m,s,t=map(int,input().split()); g=[[] for _ in range(n)]
for _ in range(m):
    u,v,w=map(int,input().split()); g[u-1].append((v-1,w))
d=[float('inf')]*n; d[s-1]=0; q=[(0,s-1)]
while q:
    du,u=heapq.heappop(q)
    if du!=d[u]: continue
    for v,w in g[u]:
        if du+w<d[v]: d[v]=du+w; heapq.heappush(q,(d[v],v))
print(-1 if d[t-1]==float('inf') else d[t-1])
