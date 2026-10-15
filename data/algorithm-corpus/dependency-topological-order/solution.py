import heapq
n,m=map(int,input().split()); g=[[] for _ in range(n)]; d=[0]*n
for _ in range(m):
    u,v=map(int,input().split()); g[u-1].append(v-1); d[v-1]+=1
q=[i for i in range(n) if d[i]==0]; heapq.heapify(q); ans=[]
while q:
    u=heapq.heappop(q); ans.append(u+1)
    for v in g[u]:
        d[v]-=1
        if d[v]==0: heapq.heappush(q,v)
print(' '.join(map(str,ans)) if len(ans)==n else 'IMPOSSIBLE')
