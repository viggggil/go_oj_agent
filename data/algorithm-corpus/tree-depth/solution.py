from collections import deque
n=int(input()); g=[[] for _ in range(n)]
for _ in range(n-1):
    u,v=map(int,input().split()); g[u-1].append(v-1); g[v-1].append(u-1)
d=[0]*n; d[0]=1; q=deque([0])
while q:
    u=q.popleft()
    for v in g[u]:
        if not d[v]: d[v]=d[u]+1; q.append(v)
print(max(d))
