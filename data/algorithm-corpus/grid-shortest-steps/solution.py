from collections import deque
n,m=map(int,input().split()); a=[input().strip() for _ in range(n)]; d=[[-1]*m for _ in range(n)]; q=deque()
if a[0][0]=='0': d[0][0]=0; q.append((0,0))
while q:
    x,y=q.popleft()
    for dx,dy in ((1,0),(-1,0),(0,1),(0,-1)):
        u,v=x+dx,y+dy
        if 0<=u<n and 0<=v<m and a[u][v]=='0' and d[u][v]<0: d[u][v]=d[x][y]+1; q.append((u,v))
print(d[-1][-1])
